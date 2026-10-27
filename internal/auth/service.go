// Package auth registers users, logs them in and rotates their tokens.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	minPasswordLen = 10
	maxPasswordLen = 200
	maxEmailLen    = 254
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrWeakPassword       = fmt.Errorf("password must be %d to %d characters", minPasswordLen, maxPasswordLen)
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

// dummyHash is checked when the email is unknown, so both cases take the same time.
var dummyHash = HashPassword("timing-equalizer")

// Tokens is what a successful login or refresh returns.
type Tokens struct {
	Access         string    `json:"accessToken"`
	AccessExpires  time.Time `json:"accessExpiresAt"`
	Refresh        string    `json:"-"`
	RefreshExpires time.Time `json:"-"`
}

// Service holds the auth dependencies.
type Service struct {
	db     *pgxpool.Pool
	kek    *vault.KEK
	secret []byte
}

func NewService(db *pgxpool.Pool, kek *vault.KEK, jwtSecret []byte) *Service {
	return &Service{db: db, kek: kek, secret: jwtSecret}
}

// Register creates a user with a fresh data key.
func (s *Service) Register(ctx context.Context, email, password string) (uuid.UUID, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return uuid.Nil, err
	}
	if n := utf8.RuneCountInString(password); n < minPasswordLen || n > maxPasswordLen {
		return uuid.Nil, ErrWeakPassword
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	return id, insertUser(ctx, s.db, id, email, HashPassword(password), s.kek.NewWrappedDEK(id))
}

// Login checks the credentials and starts a new token family.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (Tokens, error) {
	email, _ = normalizeEmail(email)
	id, hash, err := findCredentials(ctx, s.db, email)
	if err != nil {
		_, _ = CheckPassword(password, dummyHash)
		return Tokens{}, ErrInvalidCredentials
	}
	if ok, err := CheckPassword(password, hash); err != nil || !ok {
		return Tokens{}, ErrInvalidCredentials
	}
	return s.issue(ctx, id, uuid.New(), userAgent)
}

// Refresh exchanges a refresh token for new tokens. A token used twice means it was stolen: its family is revoked.
func (s *Service) Refresh(ctx context.Context, token, userAgent string) (Tokens, error) {
	row, err := useRefreshToken(ctx, s.db, hashToken(token))
	if err != nil {
		return Tokens{}, ErrInvalidToken
	}
	if row.reused {
		_ = revokeFamily(ctx, s.db, row.familyID)
		return Tokens{}, ErrInvalidToken
	}
	return s.issue(ctx, row.userID, row.familyID, userAgent)
}

// Logout revokes the token's family (this device's session).
func (s *Service) Logout(ctx context.Context, token string) error {
	return revokeByToken(ctx, s.db, hashToken(token))
}

// Verify returns the user id of a valid access token.
func (s *Service) Verify(token string) (uuid.UUID, error) {
	return parseAccess(s.secret, token)
}

func (s *Service) issue(ctx context.Context, userID, familyID uuid.UUID, userAgent string) (Tokens, error) {
	now := time.Now()
	access, err := signAccess(s.secret, userID, now)
	if err != nil {
		return Tokens{}, err
	}
	refresh, hash := newRefreshToken()
	t := Tokens{Access: access, AccessExpires: now.Add(accessTTL), Refresh: refresh, RefreshExpires: now.Add(refreshTTL)}
	if err := insertRefreshToken(ctx, s.db, userID, familyID, hash, t.RefreshExpires, userAgent); err != nil {
		return Tokens{}, err
	}
	return t, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > maxEmailLen {
		return "", ErrInvalidEmail
	}
	return email, nil
}
