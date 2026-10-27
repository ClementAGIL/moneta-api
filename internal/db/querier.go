// Querier lets stores run on the pool or inside a transaction, and helpers shared by every store.
package db

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
)

// Querier is implemented by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
)

// IsUnique reports a unique constraint violation.
func IsUnique(err error) bool { return pgCode(err) == uniqueViolation }

// IsForeignKey reports a foreign key violation (e.g. deleting a row still referenced).
func IsForeignKey(err error) bool { return pgCode(err) == foreignKeyViolation }

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// NotFoundIfNoRows turns pgx.ErrNoRows into apperr.NotFound(what).
func NotFoundIfNoRows(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound(what)
	}
	return err
}

// Affected returns NotFound when a write touched no row (wrong id or another user's row).
func Affected(tag pgconn.CommandTag, err error, what string) error {
	if err == nil && tag.RowsAffected() == 0 {
		return apperr.NotFound(what)
	}
	return err
}

// CleanName trims a user-entered name and checks its length.
func CleanName(field, name string, maxLen int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxLen {
		return "", apperr.Invalid(field, "must be 1 to %d characters", maxLen)
	}
	return name, nil
}
