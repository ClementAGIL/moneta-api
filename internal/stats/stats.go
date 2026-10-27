// Package stats aggregates spending and income. Internal transfers and categories excluded from stats are ignored;
// amounts in expense categories are netted (a refund reduces the category's spending).
package stats

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	monthLayout   = "2006-01"
	averageMonths = 6
)

// counted is the FROM/WHERE shared by every aggregate: the user's transactions that are real spending or income.
const counted = ` FROM transactions t LEFT JOIN categories c ON c.id = t.category_id
	WHERE t.user_id = $1 AND t.transfer_id IS NULL
	  AND (c.id IS NULL OR (c.kind <> 'transfer' AND NOT c.excluded_from_stats))`

// Totals are positive amounts in cents.
type Totals struct {
	Income   int64 `json:"incomeCents"`
	Spending int64 `json:"spendingCents"`
}

// CategoryAmount is a top-level category's net spending (CategoryID nil = uncategorized).
type CategoryAmount struct {
	CategoryID *uuid.UUID `json:"categoryId"`
	Spending   int64      `json:"spendingCents"`
	Count      int        `json:"count"`
}

type Summary struct {
	Month         string           `json:"month"`
	Totals        Totals           `json:"totals"`
	Previous      Totals           `json:"previous"`
	Average       Totals           `json:"average6"` // monthly average over the 6 months before
	SavingsRate   *float64         `json:"savingsRate"`
	ByCategory    []CategoryAmount `json:"byCategory"`
	Uncategorized int              `json:"uncategorizedCount"`
}

// MonthAmount is one point of a category series.
type MonthAmount struct {
	Month      string     `json:"month"`
	CategoryID *uuid.UUID `json:"categoryId"`
	Spending   int64      `json:"spendingCents"`
	Income     int64      `json:"incomeCents"`
}

type Merchant struct {
	Name     string `json:"name"`
	Spending int64  `json:"spendingCents"`
	Count    int    `json:"count"`
}

type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

type Service struct {
	db   *pgxpool.Pool
	keys KeySource
}

func NewService(db *pgxpool.Pool, keys KeySource) *Service { return &Service{db: db, keys: keys} }

// Summary returns the month's totals compared with the previous month and the 6-month average.
func (s *Service) Summary(ctx context.Context, userID uuid.UUID, month time.Time) (Summary, error) {
	sum := Summary{Month: month.Format(monthLayout)}
	var err error
	next := month.AddDate(0, 1, 0)
	if sum.Totals, err = s.totals(ctx, userID, month, next); err != nil {
		return sum, err
	}
	if sum.Previous, err = s.totals(ctx, userID, month.AddDate(0, -1, 0), month); err != nil {
		return sum, err
	}
	avg, err := s.totals(ctx, userID, month.AddDate(0, -averageMonths, 0), month)
	if err != nil {
		return sum, err
	}
	sum.Average = Totals{Income: avg.Income / averageMonths, Spending: avg.Spending / averageMonths}
	if sum.Totals.Income > 0 {
		rate := float64(sum.Totals.Income-sum.Totals.Spending) / float64(sum.Totals.Income)
		sum.SavingsRate = &rate
	}
	if sum.ByCategory, err = s.byCategory(ctx, userID, month, next); err != nil {
		return sum, err
	}
	err = s.db.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE user_id = $1 AND category_id IS NULL
		AND transfer_id IS NULL AND booked_on >= $2 AND booked_on < $3`, userID, month, next).Scan(&sum.Uncategorized)
	return sum, err
}

func (s *Service) totals(ctx context.Context, userID uuid.UUID, from, to time.Time) (Totals, error) {
	var t Totals
	err := s.db.QueryRow(ctx, `SELECT
		coalesce(sum(t.amount_cents) FILTER (WHERE t.amount_cents > 0 AND coalesce(c.kind, '') <> 'expense'), 0),
		coalesce(-sum(t.amount_cents) FILTER (WHERE t.amount_cents < 0 OR c.kind = 'expense'), 0)`+counted+`
		AND t.booked_on >= $2 AND t.booked_on < $3`, userID, from, to).Scan(&t.Income, &t.Spending)
	return t, err
}

func (s *Service) byCategory(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]CategoryAmount, error) {
	rows, err := s.db.Query(ctx, `SELECT coalesce(c.parent_id, c.id), -sum(t.amount_cents), count(*)`+counted+`
		AND t.booked_on >= $2 AND t.booked_on < $3 AND (c.kind = 'expense' OR (c.id IS NULL AND t.amount_cents < 0))
		GROUP BY 1 ORDER BY 2 DESC`, userID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CategoryAmount, error) {
		var a CategoryAmount
		return a, row.Scan(&a.CategoryID, &a.Spending, &a.Count)
	})
}

// Categories returns monthly spending and income per top-level category between two months (inclusive).
func (s *Service) Categories(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]MonthAmount, error) {
	rows, err := s.db.Query(ctx, `SELECT to_char(t.booked_on, 'YYYY-MM'), coalesce(c.parent_id, c.id),
		coalesce(-sum(t.amount_cents) FILTER (WHERE c.kind = 'expense' OR (c.id IS NULL AND t.amount_cents < 0)), 0),
		coalesce(sum(t.amount_cents) FILTER (WHERE c.kind = 'income' OR (c.id IS NULL AND t.amount_cents > 0)), 0)`+
		counted+` AND t.booked_on >= $2 AND t.booked_on < $3 GROUP BY 1, 2 ORDER BY 1, 3 DESC`,
		userID, from, to.AddDate(0, 1, 0))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MonthAmount, error) {
		var m MonthAmount
		return m, row.Scan(&m.Month, &m.CategoryID, &m.Spending, &m.Income)
	})
}

// Merchants returns the merchants the user spent the most with between two dates.
func (s *Service) Merchants(ctx context.Context, userID uuid.UUID, from, to time.Time, limit int) ([]Merchant, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT (array_agg(t.merchant_enc))[1], -sum(t.amount_cents), count(*)`+counted+`
		AND t.amount_cents < 0 AND t.booked_on >= $2 AND t.booked_on < $3
		GROUP BY t.merchant_key ORDER BY 2 DESC LIMIT $4`, userID, from, to, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Merchant, error) {
		var m Merchant
		var nameEnc []byte
		if err := row.Scan(&nameEnc, &m.Spending, &m.Count); err != nil {
			return m, err
		}
		m.Name, err = keys.Open(nameEnc)
		return m, err
	})
}
