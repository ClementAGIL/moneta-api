// SQL for banks and accounts.
package accounts

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/db"
)

func (s *Store) Banks(ctx context.Context, userID uuid.UUID) ([]Bank, error) {
	rows, err := s.q.Query(ctx, `SELECT id, name, created_at FROM banks WHERE user_id = $1 ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Bank, error) {
		var b Bank
		return b, row.Scan(&b.ID, &b.Name, &b.CreatedAt)
	})
}

func (s *Store) CreateBank(ctx context.Context, userID uuid.UUID, name string) (Bank, error) {
	name, err := db.CleanName("name", name, maxNameLen)
	if err != nil {
		return Bank{}, err
	}
	b := Bank{ID: uuid.Must(uuid.NewV7()), Name: name}
	err = s.q.QueryRow(ctx, `INSERT INTO banks (id, user_id, name) VALUES ($1, $2, $3) RETURNING created_at`,
		b.ID, userID, name).Scan(&b.CreatedAt)
	return b, uniqueName(err, "bank")
}

func (s *Store) RenameBank(ctx context.Context, userID, id uuid.UUID, name string) error {
	name, err := db.CleanName("name", name, maxNameLen)
	if err != nil {
		return err
	}
	tag, err := s.q.Exec(ctx, `UPDATE banks SET name = $3 WHERE id = $2 AND user_id = $1`, userID, id, name)
	return db.Affected(tag, uniqueName(err, "bank"), "bank")
}

// DeleteBank deletes the bank with its accounts and their transactions.
func (s *Store) DeleteBank(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `DELETE FROM banks WHERE id = $2 AND user_id = $1`, userID, id)
	return db.Affected(tag, err, "bank")
}

func (s *Store) Accounts(ctx context.Context, userID uuid.UUID) ([]Account, error) {
	return s.listAccounts(ctx, userID, nil)
}

func (s *Store) CreateAccount(ctx context.Context, userID uuid.UUID, in AccountInput) (Account, error) {
	if err := in.validate(); err != nil {
		return Account{}, err
	}
	id := uuid.Must(uuid.NewV7())
	_, err := s.q.Exec(ctx, `INSERT INTO accounts (id, user_id, bank_id, name, kind, anchor_balance_cents, anchor_date)
		SELECT $1, $2, b.id, $4, $5, $6, $7 FROM banks b WHERE b.id = $3 AND b.user_id = $2`,
		id, userID, in.BankID, in.Name, in.Kind, in.AnchorBalance, in.AnchorDate)
	if err := uniqueName(err, "account"); err != nil {
		return Account{}, err
	}
	acc, err := s.AccountOf(ctx, userID, id)
	if err != nil {
		return Account{}, apperr.Invalid("bankId", "unknown bank") // nothing inserted: the bank is not the user's
	}
	return acc, nil
}

func (s *Store) UpdateAccount(ctx context.Context, userID, id uuid.UUID, in AccountInput) (Account, error) {
	if err := in.validate(); err != nil {
		return Account{}, err
	}
	tag, err := s.q.Exec(ctx, `UPDATE accounts SET name = $3, kind = $4, anchor_balance_cents = $5, anchor_date = $6
		WHERE id = $2 AND user_id = $1`, userID, id, in.Name, in.Kind, in.AnchorBalance, in.AnchorDate)
	if err := db.Affected(tag, uniqueName(err, "account"), "account"); err != nil {
		return Account{}, err
	}
	return s.AccountOf(ctx, userID, id)
}

func (s *Store) DeleteAccount(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `DELETE FROM accounts WHERE id = $2 AND user_id = $1`, userID, id)
	return db.Affected(tag, err, "account")
}

// listAccounts lists the user's accounts (or only one) with their current balance.
func (s *Store) listAccounts(ctx context.Context, userID uuid.UUID, only *uuid.UUID) ([]Account, error) {
	rows, err := s.q.Query(ctx, `
		SELECT a.id, a.bank_id, a.name, a.kind, a.currency, a.anchor_balance_cents, a.anchor_date::text,
		       a.anchor_balance_cents + `+balanceSQL+`
		FROM accounts a JOIN banks b ON b.id = a.bank_id
		WHERE a.user_id = $1 AND ($2::uuid IS NULL OR a.id = $2)
		ORDER BY b.name, a.name`, userID, only)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Account, error) {
		var a Account
		return a, row.Scan(&a.ID, &a.BankID, &a.Name, &a.Kind, &a.Currency, &a.AnchorBalance, &a.AnchorDate, &a.Balance)
	})
}

func uniqueName(err error, what string) error {
	if db.IsUnique(err) {
		return apperr.Conflict("a %s with this name already exists", what)
	}
	return err
}
