// Worker loop: claim one due job at a time, run its handler, then mark it done or schedule a retry.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	pollInterval = 2 * time.Second
	lease        = 5 * time.Minute // a crashed worker's job becomes claimable again after this
	retryBase    = 30 * time.Second
	maxRetryWait = 6 * time.Hour
)

// Handler runs one job; an error schedules a retry with exponential backoff.
type Handler func(ctx context.Context, job Job) error

// Worker runs the handlers registered per job kind.
type Worker struct {
	pool     *pgxpool.Pool
	logger   *slog.Logger
	handlers map[string]Handler
}

func NewWorker(pool *pgxpool.Pool, logger *slog.Logger) *Worker {
	return &Worker{pool: pool, logger: logger, handlers: map[string]Handler{}}
}

// Handle registers the handler for a job kind.
func (w *Worker) Handle(kind string, h Handler) {
	w.handlers[kind] = h
}

// Run processes jobs until ctx is cancelled, draining every due job before sleeping.
func (w *Worker) Run(ctx context.Context) {
	for {
		for w.runOne(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// runOne returns true when a job was claimed, so the caller keeps going without sleeping.
func (w *Worker) runOne(ctx context.Context) bool {
	job, err := w.claim(ctx)
	if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil {
		return false
	}
	if err != nil {
		w.logger.Error("claiming job", "err", err)
		return false
	}
	err = w.execute(ctx, job)
	if err := w.finish(ctx, job, err); err != nil {
		w.logger.Error("finishing job", "id", job.ID, "err", err)
	}
	return true
}

func (w *Worker) claim(ctx context.Context) (Job, error) {
	var job Job
	err := w.pool.QueryRow(ctx, `
		UPDATE jobs SET attempts = attempts + 1, locked_until = now() + make_interval(secs => $1)
		WHERE id = (
			SELECT id FROM jobs
			WHERE done_at IS NULL AND run_at <= now() AND attempts < max_attempts
			  AND (locked_until IS NULL OR locked_until < now())
			ORDER BY run_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING id, kind, user_id, payload, attempts`, lease.Seconds(),
	).Scan(&job.ID, &job.Kind, &job.UserID, &job.Payload, &job.Attempts)
	return job, err
}

func (w *Worker) execute(ctx context.Context, job Job) error {
	handler, ok := w.handlers[job.Kind]
	if !ok {
		return fmt.Errorf("no handler for job kind %q", job.Kind)
	}
	w.logger.Info("job started", "id", job.ID, "kind", job.Kind, "attempt", job.Attempts)
	return handler(ctx, job)
}

func (w *Worker) finish(ctx context.Context, job Job, runErr error) error {
	if runErr == nil {
		_, err := w.pool.Exec(ctx, `UPDATE jobs SET done_at = now(), locked_until = NULL WHERE id = $1`, job.ID)
		return err
	}
	w.logger.Warn("job failed", "id", job.ID, "kind", job.Kind, "attempt", job.Attempts, "err", runErr)
	_, err := w.pool.Exec(ctx,
		`UPDATE jobs SET last_error = $2, run_at = now() + make_interval(secs => $3), locked_until = NULL WHERE id = $1`,
		job.ID, runErr.Error(), backoff(job.Attempts).Seconds())
	return err
}

// backoff doubles the wait after each failed attempt: 30 s, 1 min, 2 min... capped at maxRetryWait.
func backoff(attempts int) time.Duration {
	wait := retryBase << max(attempts-1, 0)
	if wait <= 0 || wait > maxRetryWait {
		return maxRetryWait
	}
	return wait
}
