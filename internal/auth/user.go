// The signed-in user's profile and data keys.
package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/vault"
)

// User is the public profile returned by GET /me.
type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"createdAt"`
}

// Me returns the profile of userID.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (User, error) {
	var u User
	err := s.db.QueryRow(ctx, `SELECT id, email, locale, created_at FROM users WHERE id = $1`, userID).
		Scan(&u.ID, &u.Email, &u.Locale, &u.CreatedAt)
	return u, err
}

// Keys returns the user's encryption keys (unwrapped from the stored DEK).
func (s *Service) Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error) {
	var wrapped []byte
	if err := s.db.QueryRow(ctx, `SELECT dek_wrapped FROM users WHERE id = $1`, userID).Scan(&wrapped); err != nil {
		return nil, err
	}
	return s.kek.Unwrap(userID, wrapped)
}
