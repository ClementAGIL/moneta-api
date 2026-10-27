// Dependency wiring: builds the services, connects their hooks and registers the job handlers.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/auth"
	"github.com/ClementAGIL/moneta-api/internal/categories"
	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/config"
	"github.com/ClementAGIL/moneta-api/internal/httpapi"
	"github.com/ClementAGIL/moneta-api/internal/imports"
	"github.com/ClementAGIL/moneta-api/internal/jobs"
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
}

func wire(pool *pgxpool.Pool, kek *vault.KEK, cfg config.Config, logger *slog.Logger) (httpapi.Deps, *jobs.Worker) {
	authSvc := auth.NewService(pool, kek, cfg.JWTSecret, categories.Seed)
	svc := services{
		auth:        authSvc,
		categorizer: categorize.NewService(pool, authSvc, nil),
		transfers:   transfers.NewService(pool),
		recurring:   recurring.NewService(pool, authSvc),
	}
	queueAnalysis := func(ctx context.Context, userID uuid.UUID) error {
		return jobs.Enqueue(ctx, pool, analyzeJob, &userID, struct{}{}, time.Time{})
	}
	afterImport := func(ctx context.Context, userID, importID uuid.UUID) error {
		if err := svc.categorizer.Recategorize(ctx, userID, &importID); err != nil {
			return err
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
		Analyze:      queueAnalysis,
		WebDir:       cfg.WebDir,
		CookieSecure: cfg.CookieSecure,
	}
	return deps, svc.worker(pool, logger)
}

func (svc services) worker(pool *pgxpool.Pool, logger *slog.Logger) *jobs.Worker {
	worker := jobs.NewWorker(pool, logger)
	worker.Handle(categorize.JobKind, func(ctx context.Context, job jobs.Job) error {
		return svc.categorizer.Recategorize(ctx, *job.UserID, nil)
	})
	worker.Handle(analyzeJob, func(ctx context.Context, job jobs.Job) error {
		if _, err := svc.transfers.Detect(ctx, *job.UserID); err != nil {
			return err
		}
		return svc.recurring.Refresh(ctx, *job.UserID)
	})
	return worker
}
