// Package jobs is a PostgreSQL job queue: Enqueue inserts work, a Worker claims and runs it with retries.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is satisfied by the pool and by a transaction, so a job can be queued in the same transaction as the
// data it refers to (e.g. an import commit).
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Job is one claimed unit of work handed to a Handler.
type Job struct {
	ID       uuid.UUID
	Kind     string
	UserID   *uuid.UUID
	Payload  json.RawMessage
	Attempts int // including the current one
}

// Enqueue schedules a job of the given kind to run at runAt (time.Time{} = now).
func Enqueue(ctx context.Context, db Execer, kind string, userID *uuid.UUID, payload any, runAt time.Time) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding %s payload: %w", kind, err)
	}
	if runAt.IsZero() {
		runAt = time.Now()
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("new job id: %w", err)
	}
	_, err = db.Exec(ctx,
		`INSERT INTO jobs (id, kind, user_id, payload, run_at) VALUES ($1, $2, $3, $4, $5)`,
		id, kind, userID, body, runAt)
	if err != nil {
		return fmt.Errorf("enqueuing %s: %w", kind, err)
	}
	return nil
}
