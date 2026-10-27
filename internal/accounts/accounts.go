// Package accounts stores the user's banks and their accounts.
package accounts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/db"
)

const (
	maxNameLen = 80
	dateLayout = "2006-01-02"
)

// balanceSQL sums the account's movements after its anchor date (account aliased "a").
const balanceSQL = `0`

var kinds = map[string]bool{"current": true, "card": true, "savings": true, "other": true}

type Bank struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Account struct {
	ID            uuid.UUID `json:"id"`
	BankID        uuid.UUID `json:"bankId"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Currency      string    `json:"currency"`
	AnchorBalance int64     `json:"anchorBalanceCents"`
	AnchorDate    string    `json:"anchorDate"`   // YYYY-MM-DD
	Balance       int64     `json:"balanceCents"` // anchor + transactions after the anchor date
}

// AccountInput is the writable part of an account (create and update).
type AccountInput struct {
	BankID        uuid.UUID `json:"bankId"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	AnchorBalance int64     `json:"anchorBalanceCents"`
	AnchorDate    string    `json:"anchorDate"`
}

func (in *AccountInput) validate() error {
	name, err := db.CleanName("name", in.Name, maxNameLen)
	if err != nil {
		return err
	}
	in.Name = name
	if !kinds[in.Kind] {
		return apperr.Invalid("kind", "must be current, card, savings or other")
	}
	if in.AnchorDate == "" {
		in.AnchorDate = time.Now().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, in.AnchorDate); err != nil {
		return apperr.Invalid("anchorDate", "must be a date like 2026-01-31")
	}
	return nil
}

// Store runs the SQL for banks and accounts.
type Store struct{ q db.Querier }

func NewStore(q db.Querier) *Store { return &Store{q: q} }

// AccountOf checks that accountID belongs to userID.
func (s *Store) AccountOf(ctx context.Context, userID, accountID uuid.UUID) (Account, error) {
	accounts, err := s.listAccounts(ctx, userID, &accountID)
	if err != nil {
		return Account{}, err
	}
	if len(accounts) == 0 {
		return Account{}, apperr.NotFound("account")
	}
	return accounts[0], nil
}
