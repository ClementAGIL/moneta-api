// Forecast inputs read from the database: current balance, recurring items still due, daily net history.
package forecast

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/vault"
)

// accountFilter selects the account, or every current and card account (savings are not spent from).
const accountFilter = `($2::uuid IS NULL AND a.kind IN ('current', 'card') OR a.id = $2)`

func (s *Service) balance(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID) (int64, error) {
	var balance int64
	err := s.db.QueryRow(ctx, `SELECT coalesce(sum(a.anchor_balance_cents + coalesce((SELECT sum(t.amount_cents)
		FROM transactions t WHERE t.account_id = a.id AND t.booked_on > a.anchor_date), 0)), 0)
		FROM accounts a WHERE a.user_id = $1 AND `+accountFilter, userID, accountID).Scan(&balance)
	return balance, err
}

// expected lists active recurring series due after today and before the end of the month, on the selected
// accounts (a series belongs to the accounts its transactions were seen on).
// Names are decrypted only when the service has keys (insights only need the amounts).
func (s *Service) expected(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID,
	today time.Time) ([]Expected, error) {
	var keys *vault.UserKeys
	if s.keys != nil {
		var err error
		if keys, err = s.keys.Keys(ctx, userID); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.Query(ctx, `SELECT r.display_enc, r.next_date, r.amount_cents FROM recurring r
		WHERE r.user_id = $1 AND NOT r.ignored AND r.next_date > $3
		  AND r.next_date < date_trunc('month', $3::date) + interval '1 month'
		  AND EXISTS (SELECT 1 FROM transactions t JOIN accounts a ON a.id = t.account_id
		              WHERE t.recurring_id = r.id AND `+accountFilter+`)
		ORDER BY r.next_date`, userID, accountID, today)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Expected, error) {
		var e Expected
		var nameEnc []byte
		var date time.Time
		if err := row.Scan(&nameEnc, &date, &e.Amount); err != nil {
			return e, err
		}
		e.Date = date.Format(time.DateOnly)
		if keys == nil {
			return e, nil
		}
		name, err := keys.Open(nameEnc)
		e.Name = name
		return e, err
	})
}

// dailyNet returns the net of non-recurring, non-transfer movements for each of the last 90 days (0 for quiet days,
// which matter as much as busy ones for the resampling).
func (s *Service) dailyNet(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID,
	today time.Time) ([]int64, error) {
	rows, err := s.db.Query(ctx, `SELECT coalesce(sum(t.amount_cents), 0)
		FROM generate_series($3::date - $4::int + 1, $3::date, interval '1 day') AS d(day)
		LEFT JOIN transactions t ON t.booked_on = d.day::date AND t.user_id = $1 AND t.recurring_id IS NULL
		  AND t.transfer_id IS NULL
		  AND t.account_id IN (SELECT a.id FROM accounts a WHERE a.user_id = $1 AND `+accountFilter+`)
		GROUP BY d.day ORDER BY d.day`, userID, accountID, today, historyDays)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
