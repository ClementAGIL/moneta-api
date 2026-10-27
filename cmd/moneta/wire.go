// Dependency wiring: builds the services, connects their hooks and registers the job handlers.
package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/auth"
	"github.com/ClementAGIL/moneta-api/internal/categories"
	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/config"
	"github.com/ClementAGIL/moneta-api/internal/forecast"
	"github.com/ClementAGIL/moneta-api/internal/httpapi"
	"github.com/ClementAGIL/moneta-api/internal/imports"
	"github.com/ClementAGIL/moneta-api/internal/insights"
	"github.com/ClementAGIL/moneta-api/internal/jobs"
	"github.com/ClementAGIL/moneta-api/internal/ml"
	"github.com/ClementAGIL/moneta-api/internal/recurring"
	"github.com/ClementAGIL/moneta-api/internal/stats"
	"github.com/ClementAGIL/moneta-api/internal/transactions"
	"github.com/ClementAGIL/moneta-api/internal/transfers"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

// analyzeJob re-runs everything derived from a user's transactions (after an import or a transfer change).
const analyzeJob = "analyze.user"

type services struct {
	auth        *auth.Service
	categorizer *categorize.Service
	transfers   *transfers.Service
	recurring   *recurring.Service
	insights    *insights.Service
	model       *ml.Service
}

// wire returns the HTTP dependencies and the function starting the background work (job worker, schedules).
func wire(pool *pgxpool.Pool, kek *vault.KEK, cfg config.Config, logger *slog.Logger) (httpapi.Deps,
	func(context.Context)) {
	authSvc := auth.NewService(pool, kek, cfg.JWTSecret, categories.Seed)
	svc := services{
		auth:        authSvc,
		categorizer: categorize.NewService(pool, authSvc),
		transfers:   transfers.NewService(pool),
		recurring:   recurring.NewService(pool, authSvc),
		insights:    insights.NewService(pool),
	}
	svc.model = ml.NewService(pool, authSvc, svc.categorizer)
	svc.categorizer.UseModel(svc.model.Predictor)
	queueAnalysis := func(ctx context.Context, userID uuid.UUID) error {
		return jobs.EnqueueOnce(ctx, pool, analyzeJob, userID)
	}
	// The import is already committed when this runs: a categorization failure is only logged (the analysis job
	// recategorizes everything anyway), so the client never retries an import that succeeded.
	afterImport := func(ctx context.Context, userID, importID uuid.UUID) error {
		if err := svc.categorizer.Recategorize(ctx, userID, &importID); err != nil {
			logger.Error("categorizing import", "import", importID, "err", err)
		}
		return queueAnalysis(ctx, userID)
	}

	deps := httpapi.Deps{
		Logger:       logger,
		DB:           pool,
		Auth:         authSvc,
		Imports:      imports.NewService(pool, authSvc, afterImport),
		Transactions: transactions.NewService(pool, authSvc),
		Categorize:   svc.categorizer,
		Stats:        stats.NewService(pool, authSvc),
		Transfers:    svc.transfers,
		Recurring:    svc.recurring,
		Insights:     svc.insights,
		Model:        svc.model,
		Forecast:     forecast.NewService(pool, authSvc),
		Analyze:      queueAnalysis,
		WebDir:       cfg.WebDir,
		CookieSecure: cfg.CookieSecure,
	}
	return deps, svc.background(pool, logger)
}

// background starts the job worker and the periodic tasks.
func (svc services) background(pool *pgxpool.Pool, logger *slog.Logger) func(context.Context) {
	return func(ctx context.Context) {
		go svc.worker(pool, logger).Run(ctx)
		go jobs.Every(ctx, logger, "housekeeping", time.Hour, func(ctx context.Context) error {
			return errors.Join(imports.Expire(ctx, pool), jobs.Purge(ctx, pool), auth.PurgeTokens(ctx, pool))
		})
		// Insights depend on the current month, so every user is re-analyzed daily even without new imports.
		go jobs.Every(ctx, logger, "analyze.daily", 24*time.Hour, func(ctx context.Context) error {
			return queueEveryone(ctx, pool)
		})
	}
}

func queueEveryone(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `INSERT INTO jobs (id, kind, user_id) SELECT gen_random_uuid(), $1, u.id FROM users u
		WHERE EXISTS (SELECT 1 FROM transactions t WHERE t.user_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.user_id = u.id AND j.kind = $1
		    AND j.done_at IS NULL AND j.attempts < j.max_attempts)`, analyzeJob)
	return err
}

func (svc services) worker(pool *pgxpool.Pool, logger *slog.Logger) *jobs.Worker {
	worker := jobs.NewWorker(pool, logger)
	worker.Handle(categorize.JobKind, func(ctx context.Context, job jobs.Job) error {
		return svc.categorizer.Recategorize(ctx, *job.UserID, nil)
	})
	worker.Handle(ml.JobKind, func(ctx context.Context, job jobs.Job) error {
		if err := svc.model.Retrain(ctx, *job.UserID); err != nil {
			return err
		}
		return svc.categorizer.Recategorize(ctx, *job.UserID, nil)
	})
	worker.Handle(analyzeJob, svc.analyze)
	return worker
}

// analyze: transfers first (they leave the stats), then the model learns from the categorized rest and fills the
// gaps, then recurring series and insights see the final categories.
func (svc services) analyze(ctx context.Context, job jobs.Job) error {
	userID := *job.UserID
	if _, err := svc.transfers.Detect(ctx, userID); err != nil {
		return err
	}
	if err := svc.model.Retrain(ctx, userID); err != nil {
		return err
	}
	if err := svc.categorizer.Recategorize(ctx, userID, nil); err != nil {
		return err
	}
	if err := svc.recurring.Refresh(ctx, userID); err != nil {
		return err
	}
	month := thisMonth()
	if err := svc.insights.Generate(ctx, userID, month.AddDate(0, -1, 0)); err != nil {
		return err
	}
	return svc.insights.Generate(ctx, userID, month)
}

func thisMonth() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
