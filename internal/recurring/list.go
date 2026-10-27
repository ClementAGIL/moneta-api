// Reading recurring series for the API, and the user's choices (ignore, subscription yes/no).
package recurring

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/db"
)

// Item is a recurring series as the clients see it.
type Item struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Direction      string     `json:"direction"` // in or out
	Cadence        string     `json:"cadence"`
	Amount         int64      `json:"amountCents"`
	MonthlyCost    int64      `json:"monthlyCents"`
	PreviousAmount *int64     `json:"previousAmountCents"`
	PriceChangedOn *string    `json:"priceChangedOn"`
	Occurrences    int        `json:"occurrences"`
	LastDate       string     `json:"lastDate"`
	NextDate       string     `json:"nextDate"`
	CategoryID     *uuid.UUID `json:"categoryId"`
	Subscription   bool       `json:"subscription"`
	Ignored        bool       `json:"ignored"`
	Active         bool       `json:"active"`
}

// Update holds the user's choices; nil fields are unchanged.
type Update struct {
	Ignored      *bool `json:"ignored"`
	Subscription *bool `json:"subscription"`
}

// List returns all series, active ones first, then by monthly cost.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Item, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, display_enc, sign, cadence, amount_cents, previous_amount_cents,
		price_changed_on::text, occurrences, last_date, next_date, category_id, coalesce(subscription_override,
		subscription), ignored FROM recurring WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) {
		var it Item
		var nameEnc []byte
		var last, next time.Time
		err := row.Scan(&it.ID, &nameEnc, &it.Direction, &it.Cadence, &it.Amount, &it.PreviousAmount,
			&it.PriceChangedOn, &it.Occurrences, &last, &next, &it.CategoryID, &it.Subscription, &it.Ignored)
		if err != nil {
			return it, err
		}
		it.Name, err = keys.Open(nameEnc)
		it.LastDate, it.NextDate = last.Format(time.DateOnly), next.Format(time.DateOnly)
		it.MonthlyCost = MonthlyCost(it.Cadence, it.Amount)
		it.Active = !it.Ignored && !(Series{LastDate: last, NextDate: next}).Ended(now())
		return it, err
	})
	sortItems(items)
	return items, err
}

// Set records the user's choices on a series.
func (s *Service) Set(ctx context.Context, userID, id uuid.UUID, u Update) error {
	tag, err := s.db.Exec(ctx, `UPDATE recurring SET ignored = coalesce($3, ignored),
		subscription_override = coalesce($4, subscription_override), updated_at = now()
		WHERE id = $1 AND user_id = $2`, id, userID, u.Ignored, u.Subscription)
	return db.Affected(tag, err, "recurring series")
}

func sortItems(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return int(abs(b.MonthlyCost) - abs(a.MonthlyCost))
	})
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
