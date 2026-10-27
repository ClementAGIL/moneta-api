// SQL for users and refresh tokens.
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/db"
)

type refreshRow struct {
	userID   uuid.UUID
	familyID uuid.UUID
	reused   bool
}

func insertUser(ctx context.Context, q db.Querier, id uuid.UUID, email, hash string, dek []byte) error {
	_, err := q.Exec(ctx, `INSERT INTO users (id, email, password_hash, dek_wrapped) VALUES ($1, $2, $3, $4)`,
		id, email, hash, dek)
	if db.IsUnique(err) {
		return ErrEmailTaken
	}
	return err
}

func findCredentials(ctx context.Context, db *pgxpool.Pool, email string) (uuid.UUID, string, error) {
	var id uuid.UUID
	var hash string
	err := db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, email).Scan(&id, &hash)
	return id, hash, err
}

func insertRefreshToken(ctx context.Context, db *pgxpool.Pool, userID, familyID uuid.UUID, hash []byte,
	expires time.Time, userAgent string) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, userID, familyID, hash, expires, userAgent)
	return err
}

// useRefreshToken marks a valid token as used in one statement, so two concurrent refreshes cannot both succeed.
// A token that exists but was already used comes back with reused = true.
func useRefreshToken(ctx context.Context, db *pgxpool.Pool, hash []byte) (refreshRow, error) {
	var row refreshRow
	err := db.QueryRow(ctx, `
		UPDATE refresh_tokens SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		RETURNING user_id, family_id`, hash).Scan(&row.userID, &row.familyID)
	if !errors.Is(err, pgx.ErrNoRows) {
		return row, err
	}
	err = db.QueryRow(ctx, `SELECT user_id, family_id FROM refresh_tokens WHERE token_hash = $1 AND used_at IS NOT NULL`,
		hash).Scan(&row.userID, &row.familyID)
	row.reused = err == nil
	return row, err
}

func revokeFamily(ctx context.Context, db *pgxpool.Pool, familyID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`,
		familyID)
	return err
}

func revokeByToken(ctx context.Context, db *pgxpool.Pool, hash []byte) error {
	_, err := db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now()
		WHERE revoked_at IS NULL AND family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`, hash)
	return err
}
