// Commit (staged rows → transactions) and delete (cancel an open import, or undo a committed one).
package imports

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/labels"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

// Commit inserts the new rows plus the review rows the user chose to keep, then drops the staged data.
func (s *Service) Commit(ctx context.Context, userID, id uuid.UUID, keep []int) (Summary, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		accountID, err := lockStatus(ctx, tx, userID, id, "mapped")
		if err != nil {
			return err
		}
		n, err := insertTransactions(ctx, tx, keys, userID, accountID, id, keep)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE imports SET status = 'committed', grid_enc = NULL, committed_at = now(),
			rows_new = $2 WHERE id = $1`, id, n)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM import_rows WHERE import_id = $1`, id)
		return err
	})
	if err != nil {
		return Summary{}, err
	}
	if s.onCommit != nil {
		if err := s.onCommit(ctx, userID, id); err != nil {
			return Summary{}, err
		}
	}
	return s.Get(ctx, userID, id)
}

// Delete cancels an import not committed yet, or undoes a committed one (its transactions are deleted).
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM imports WHERE id = $1 AND user_id = $2 FOR UPDATE`, id, userID).
			Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("import")
		}
		if err != nil || status == "cancelled" {
			return err
		}
		if status == "committed" {
			if _, err := tx.Exec(ctx, `DELETE FROM transactions WHERE import_id = $1 AND user_id = $2`, id, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM import_rows WHERE import_id = $1`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE imports SET status = 'cancelled', grid_enc = NULL WHERE id = $1`, id)
		return err
	})
}

// lockStatus locks the import and checks its status; returns its account.
func lockStatus(ctx context.Context, tx pgx.Tx, userID, id uuid.UUID, want string) (uuid.UUID, error) {
	var status string
	var accountID *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT status, account_id FROM imports WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		id, userID).Scan(&status, &accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("import")
	}
	if err != nil {
		return uuid.Nil, err
	}
	if status != want || accountID == nil {
		return uuid.Nil, apperr.Conflict("the import must be %s first (it is %s)", want, status)
	}
	return *accountID, nil
}

type newTxn struct {
	date       time.Time
	amount     int64
	label      string
	key        []byte
	occurrence int
}

func insertTransactions(ctx context.Context, tx pgx.Tx, keys *vault.UserKeys, userID, accountID, importID uuid.UUID,
	keep []int) (int, error) {
	rows, err := tx.Query(ctx, `SELECT booked_on, amount_cents, label_enc, dedup_key, occurrence FROM import_rows
		WHERE import_id = $1 AND (status = 'new' OR (status = 'review' AND row_index = ANY($2)))
		ORDER BY row_index`, importID, keep)
	if err != nil {
		return 0, err
	}
	txns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (newTxn, error) {
		var t newTxn
		var labelEnc []byte
		if err := row.Scan(&t.date, &t.amount, &labelEnc, &t.key, &t.occurrence); err != nil {
			return t, err
		}
		t.label, err = keys.Open(labelEnc)
		return t, err
	})
	if err != nil {
		return 0, err
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"transactions"},
		[]string{"id", "user_id", "account_id", "import_id", "booked_on", "amount_cents", "label_enc", "merchant_enc",
			"merchant_key", "dedup_key", "occurrence"},
		pgx.CopyFromSlice(len(txns), func(i int) ([]any, error) {
			t := txns[i]
			merchant := labels.Merchant(labels.Normalize(t.label))
			return []any{uuid.Must(uuid.NewV7()), userID, accountID, importID, t.date, t.amount, keys.Seal(t.label),
				keys.Seal(merchant), keys.Index(merchant), t.key, t.occurrence}, nil
		}))
	return int(n), err
}
