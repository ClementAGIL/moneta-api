// More insight rules: overlapping subscriptions, unusual expense, bank fees, savings rate.
package insights

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/forecast"
)

// duplicateSubscriptions flags several active subscriptions in the same category (e.g. three streaming services).
func duplicateSubscriptions(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, _ time.Time) ([]Insight, error) {
	rows, err := db.Query(ctx, `SELECT category_id::text, count(*), -sum(CASE cadence WHEN 'yearly' THEN amount_cents / 12
		WHEN 'quarterly' THEN amount_cents / 3 WHEN 'weekly' THEN amount_cents * 52 / 12 ELSE amount_cents END)
		FROM recurring WHERE user_id = $1 AND NOT ignored AND coalesce(subscription_override, subscription)
		  AND next_date > current_date - 15 AND category_id IS NOT NULL
		GROUP BY category_id HAVING count(*) >= 2`, userID)
	if err != nil {
		return nil, err
	}
	var found []Insight
	var category string
	var count, monthly int64
	_, err = pgx.ForEachRow(rows, []any{&category, &count, &monthly}, func() error {
		found = append(found, newInsight("duplicate_subscriptions", "info", category,
			map[string]any{"categoryId": category, "count": count, "monthlyCents": monthly}))
		return nil
	})
	return found, err
}

// largeExpense points at the month's biggest expense when it stands out.
func largeExpense(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	var id string
	var amount int64
	var median float64
	err := db.QueryRow(ctx, `WITH m AS (SELECT t.id, -t.amount_cents AS spent`+counted+` AND t.amount_cents < 0
		  AND t.recurring_id IS NULL
		  AND t.booked_on >= $2 AND t.booked_on < $2::date + interval '1 month')
		SELECT id::text, spent, (SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY spent) FROM m)
		FROM m ORDER BY spent DESC LIMIT 1`, userID, month).Scan(&id, &amount, &median)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || amount < largeMinCents || float64(amount) < median*largeFactor {
		return nil, err
	}
	return []Insight{newInsight("large_expense", "info", id, map[string]any{"transactionId": id, "amountCents": amount})}, nil
}

// bankFees reports the bank fees paid since January (once a month, as a reminder that they can be negotiated).
func bankFees(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	var total int64
	err := db.QueryRow(ctx, `SELECT coalesce(-sum(t.amount_cents), 0) FROM transactions t
		JOIN categories c ON c.id = t.category_id
		WHERE t.user_id = $1 AND c.key = 'bank_fees' AND t.booked_on >= date_trunc('year', $2::date)
		  AND t.booked_on < $2::date + interval '1 month'`, userID, month).Scan(&total)
	if err != nil || total <= 0 {
		return nil, err
	}
	return []Insight{newInsight("bank_fees", "info", "", map[string]any{"amountCents": total, "year": month.Year()})}, nil
}

// savingsRate compares the month's savings rate with the 6 months before.
func savingsRate(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	var income, spending, pastIncome, pastSpending int64
	err := db.QueryRow(ctx, `SELECT
		coalesce(sum(t.amount_cents) FILTER (WHERE t.booked_on >= $2 AND t.amount_cents > 0), 0),
		coalesce(-sum(t.amount_cents) FILTER (WHERE t.booked_on >= $2 AND t.amount_cents < 0), 0),
		coalesce(sum(t.amount_cents) FILTER (WHERE t.booked_on < $2 AND t.amount_cents > 0), 0),
		coalesce(-sum(t.amount_cents) FILTER (WHERE t.booked_on < $2 AND t.amount_cents < 0), 0)`+counted+`
		AND t.booked_on >= $2::date - interval '6 months' AND t.booked_on < $2::date + interval '1 month'`,
		userID, month).Scan(&income, &spending, &pastIncome, &pastSpending)
	if err != nil || income <= 0 || pastIncome <= 0 {
		return nil, err
	}
	rate := float64(income-spending) / float64(income)
	average := float64(pastIncome-pastSpending) / float64(pastIncome)
	params := map[string]any{"rate": rate, "averageRate": average, "savedCents": income - spending}
	switch {
	case rate < average-savingsDropRatio:
		return []Insight{newInsight("savings_down", "warn", "", params)}, nil
	case rate > average+savingsDropRatio:
		return []Insight{newInsight("savings_up", "good", "", params)}, nil
	}
	return nil, nil
}

// forecastNegative warns when the median projected balance goes below zero before the end of the current month.
func forecastNegative(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	now := time.Now()
	if month.Year() != now.Year() || month.Month() != now.Month() {
		return nil, nil
	}
	f, err := forecast.NewService(db, nil).Month(ctx, userID, nil, now)
	if err != nil {
		return nil, err
	}
	for _, d := range f.Days {
		if d.P50 < 0 {
			return []Insight{newInsight("forecast_negative", "alert", "",
				map[string]any{"balanceCents": d.P50, "date": d.Date})}, nil
		}
	}
	return nil, nil
}

func abs[T int64 | float64](v T) T {
	if v < 0 {
		return -v
	}
	return v
}
