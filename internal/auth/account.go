// Account settings: change password or locale, delete the account.
package auth

import (
	"context"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var locales = map[string]bool{"fr": true, "en": true}

// Settings is PATCH /me; empty fields are unchanged. Changing the password needs the current one.
type Settings struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	Locale          string `json:"locale"`
}

// Update applies settings. A new password signs out every other session (all refresh tokens are revoked).
func (s *Service) Update(ctx context.Context, userID uuid.UUID, in Settings) error {
	if in.Locale != "" {
		if !locales[in.Locale] {
			return ErrInvalidLocale
		}
		if _, err := s.db.Exec(ctx, `UPDATE users SET locale = $2, updated_at = now() WHERE id = $1`, userID,
			in.Locale); err != nil {
			return err
		}
	}
	if in.NewPassword == "" {
		return nil
	}
	if n := utf8.RuneCountInString(in.NewPassword); n < minPasswordLen || n > maxPasswordLen {
		return ErrWeakPassword
	}
	if err := s.checkPassword(ctx, userID, in.CurrentPassword); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID,
			HashPassword(in.NewPassword)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
			userID)
		return err
	})
}

// Delete removes the account and, through foreign keys, everything it owns. The wrapped data key goes with the row,
// so encrypted values left in old backups can no longer be read (crypto-shredding).
func (s *Service) Delete(ctx context.Context, userID uuid.UUID, password string) error {
	if err := s.checkPassword(ctx, userID, password); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

func (s *Service) checkPassword(ctx context.Context, userID uuid.UUID, password string) error {
	var hash string
	if err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		return err
	}
	if ok, err := CheckPassword(password, hash); err != nil || !ok {
		return ErrWrongPassword
	}
	return nil
}
