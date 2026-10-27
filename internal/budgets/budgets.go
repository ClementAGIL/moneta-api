// Package budgets stores a monthly amount per category and computes the month's progress against it.
package budgets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/db"
)

// Progress is a budget with what was spent in the month (a category's spending includes its subcategories).
type Progress struct {
	CategoryID uuid.UUID `json:"categoryId"`
	Amount     int64     `json:"amountCents"`
	Spent      int64     `json:"spentCents"`
	Ratio      float64   `json:"ratio"`
}

type Store struct{ q db.Querier }

func NewStore(q db.Querier) *Store { return &Store{q: q} }

// Set creates or changes a category's budget; an amount of 0 removes it.
func (s *Store) Set(ctx context.Context, userID, categoryID uuid.UUID, amount int64) error {
	if amount < 0 {
		return apperr.Invalid("amountCents", "must be positive")
	}
	if amount == 0 {
		_, err := s.q.Exec(ctx, `DELETE FROM budgets WHERE user_id = $1 AND category_id = $2`, userID, categoryID)
		return err
	}
	tag, err := s.q.Exec(ctx, `INSERT INTO budgets (id, user_id, category_id, amount_cents)
		SELECT $1, $2, id, $4 FROM categories WHERE id = $3 AND user_id = $2 AND kind = 'expense'
		ON CONFLICT (user_id, category_id) DO UPDATE SET amount_cents = EXCLUDED.amount_cents`,
		uuid.Must(uuid.NewV7()), userID, categoryID, amount)
	if err == nil && tag.RowsAffected() == 0 {
		return apperr.Invalid("categoryId", "unknown expense category")
	}
	return err
}

// Progress returns every budget with the spending of the month starting at month.
func (s *Store) Progress(ctx context.Context, userID uuid.UUID, month time.Time) ([]Progress, error) {
	rows, err := s.q.Query(ctx, `
		SELECT b.category_id, b.amount_cents, coalesce(-sum(t.amount_cents), 0)
		FROM budgets b
		LEFT JOIN categories c ON c.user_id = b.user_id AND (c.id = b.category_id OR c.parent_id = b.category_id)
		LEFT JOIN transactions t ON t.category_id = c.id AND t.transfer_id IS NULL
		     AND t.booked_on >= $2 AND t.booked_on < ($2::date + interval '1 month')
		WHERE b.user_id = $1
		GROUP BY b.category_id, b.amount_cents
		ORDER BY 3 DESC`, userID, month)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Progress, error) {
		var p Progress
		err := row.Scan(&p.CategoryID, &p.Amount, &p.Spent)
		p.Ratio = float64(p.Spent) / float64(p.Amount)
		return p, err
	})
}
