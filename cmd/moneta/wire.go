// Dependency wiring: builds the services, connects their hooks and registers the job handlers.
package main

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/auth"
	"github.com/ClementAGIL/moneta-api/internal/categories"
	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/config"
	"github.com/ClementAGIL/moneta-api/internal/httpapi"
	"github.com/ClementAGIL/moneta-api/internal/imports"
	"github.com/ClementAGIL/moneta-api/internal/jobs"
	"github.com/ClementAGIL/moneta-api/internal/stats"
	"github.com/ClementAGIL/moneta-api/internal/transactions"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

func wire(pool *pgxpool.Pool, kek *vault.KEK, cfg config.Config, logger *slog.Logger) (httpapi.Deps, *jobs.Worker) {
	authSvc := auth.NewService(pool, kek, cfg.JWTSecret, categories.Seed)
	categorizer := categorize.NewService(pool, authSvc, nil)
	afterImport := func(ctx context.Context, userID, importID uuid.UUID) error {
		return categorizer.Recategorize(ctx, userID, &importID)
	}

	worker := jobs.NewWorker(pool, logger)
	worker.Handle(categorize.JobKind, func(ctx context.Context, job jobs.Job) error {
		return categorizer.Recategorize(ctx, *job.UserID, nil)
	})

	deps := httpapi.Deps{
		Logger:       logger,
		DB:           pool,
		Auth:         authSvc,
		Imports:      imports.NewService(pool, authSvc, afterImport),
		Transactions: transactions.NewService(pool, authSvc),
		Categorize:   categorizer,
		Stats:        stats.NewService(pool, authSvc),
		WebDir:       cfg.WebDir,
		CookieSecure: cfg.CookieSecure,
	}
	return deps, worker
}
