// The insight rules: each reads the month's data and returns zero or more insights. Add a rule = add a function.
package insights

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/budgets"
)

const (
	trendRatio       = 0.30  // ±30 % vs the 6-month average…
	trendMinCents    = 3000  // …and at least 30 €
	budgetWarnRatio  = 0.80  // budget 80 % used
	largeMinCents    = 20000 // a "large expense" is at least 200 €…
	largeFactor      = 3.0   // …and 3 times the month's median expense
	savingsDropRatio = 0.10  // savings rate 10 points under its average
)

type rule func(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error)

var rules = []rule{categoryTrends, budgetAlerts, priceIncreases, newSubscriptions, duplicateSubscriptions,
	largeExpense, bankFees, savingsRate}

// historyMonths counts the months with data in the 6 months before $2: averages divide by it, not by 6, so a short
// history does not look like a spending jump.
const historyMonths = `(SELECT count(DISTINCT date_trunc('month', h.booked_on)) FROM transactions h
	WHERE h.user_id = $1 AND h.booked_on >= $2::date - interval '6 months' AND h.booked_on < $2)`

const minHistoryMonths = 3

// counted mirrors stats: real spending/income only (no transfers, no excluded categories).
const counted = ` FROM transactions t LEFT JOIN categories c ON c.id = t.category_id
	WHERE t.user_id = $1 AND t.transfer_id IS NULL
	  AND (c.id IS NULL OR (c.kind <> 'transfer' AND NOT c.excluded_from_stats))`

// categoryTrends compares each top-level expense category with its 6-month average.
func categoryTrends(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	rows, err := db.Query(ctx, `SELECT coalesce(c.parent_id, c.id)::text,
		coalesce(-sum(t.amount_cents) FILTER (WHERE t.booked_on >= $2), 0)::bigint,
		(coalesce(-sum(t.amount_cents) FILTER (WHERE t.booked_on < $2), 0) / greatest(`+historyMonths+`, 1))::bigint,
		`+historyMonths+``+counted+`
		AND c.kind = 'expense' AND t.booked_on >= $2::date - interval '6 months'
		AND t.booked_on < $2::date + interval '1 month' GROUP BY 1`, userID, month)
	if err != nil {
		return nil, err
	}
	var found []Insight
	var category string
	var spent, average, months int64
	_, err = pgx.ForEachRow(rows, []any{&category, &spent, &average, &months}, func() error {
		diff := spent - average
		if months < minHistoryMonths || average <= 0 || abs(diff) < trendMinCents || abs(float64(diff)/float64(average)) < trendRatio {
			return nil
		}
		kind, severity := "category_up", "warn"
		if diff < 0 {
			kind, severity = "category_down", "good"
		}
		found = append(found, newInsight(kind, severity, category, map[string]any{
			"categoryId": category, "spentCents": spent, "averageCents": average}))
		return nil
	})
	return found, err
}

func budgetAlerts(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	progress, err := budgets.NewStore(db).Progress(ctx, userID, month)
	var found []Insight
	for _, p := range progress {
		params := map[string]any{"categoryId": p.CategoryID, "spentCents": p.Spent, "budgetCents": p.Amount}
		switch {
		case p.Ratio > 1:
			found = append(found, newInsight("budget_exceeded", "alert", p.CategoryID.String(), params))
		case p.Ratio >= budgetWarnRatio:
			found = append(found, newInsight("budget_warning", "warn", p.CategoryID.String(), params))
		}
	}
	return found, err
}

func priceIncreases(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	return seriesInsights(ctx, db, `SELECT id::text, amount_cents, previous_amount_cents FROM recurring
		WHERE user_id = $1 AND NOT ignored AND sign = 'out' AND amount_cents < previous_amount_cents
		  AND price_changed_on >= $2 AND price_changed_on < $2::date + interval '1 month'`,
		userID, month, "price_increase", "warn")
}

func newSubscriptions(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, month time.Time) ([]Insight, error) {
	return seriesInsights(ctx, db, `SELECT id::text, amount_cents, NULL::bigint FROM recurring
		WHERE user_id = $1 AND NOT ignored AND coalesce(subscription_override, subscription)
		  AND first_date >= $2::date - interval '2 months' AND first_date < $2::date + interval '1 month'`,
		userID, month, "new_subscription", "info")
}

func seriesInsights(ctx context.Context, db *pgxpool.Pool, sql string, userID uuid.UUID, month time.Time,
	kind, severity string) ([]Insight, error) {
	rows, err := db.Query(ctx, sql, userID, month)
	if err != nil {
		return nil, err
	}
	var found []Insight
	var id string
	var amount int64
	var previous *int64
	_, err = pgx.ForEachRow(rows, []any{&id, &amount, &previous}, func() error {
		params := map[string]any{"recurringId": id, "amountCents": amount}
		if previous != nil {
			params["previousCents"] = *previous
		}
		found = append(found, newInsight(kind, severity, id, params))
		return nil
	})
	return found, err
}
