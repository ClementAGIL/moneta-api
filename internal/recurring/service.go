// Running the detection over a user's transactions, storing the series, and reading them back.
package recurring

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const lookbackDays = 400

// subscriptionKeys are default categories whose recurring expenses count as subscriptions.
var subscriptionKeys = []string{"subscriptions", "streaming", "software", "sport", "telecom"}

type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

type Service struct {
	db   *pgxpool.Pool
	keys KeySource
}

func NewService(db *pgxpool.Pool, keys KeySource) *Service { return &Service{db: db, keys: keys} }

type txnPoint struct {
	id          uuid.UUID
	merchantKey []byte
	merchantEnc []byte
	categoryID  *uuid.UUID
	Point
}

type group struct {
	key, sign string
	txns      []txnPoint
}

// Refresh re-detects every series of the user: found ones are upserted (keeping the user's choices), vanished ones
// are deleted unless the user ignored them, and transactions are linked to their series.
func (s *Service) Refresh(ctx context.Context, userID uuid.UUID) error {
	groups, err := s.load(ctx, userID)
	if err != nil {
		return err
	}
	subCategories, err := s.subscriptionCategories(ctx, userID)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE transactions SET recurring_id = NULL WHERE user_id = $1`, userID); err != nil {
			return err
		}
		var kept []uuid.UUID
		for _, g := range groups {
			points := make([]Point, len(g.txns))
			for i, t := range g.txns {
				points[i] = t.Point
			}
			series, ok := Detect(points)
			if !ok {
				continue
			}
			id, err := upsert(ctx, tx, userID, g, series, subCategories)
			if err != nil {
				return err
			}
			kept = append(kept, id)
		}
		_, err := tx.Exec(ctx, `DELETE FROM recurring WHERE user_id = $1 AND NOT ignored AND NOT (id = ANY($2))`,
			userID, kept)
		return err
	})
}

// load groups the last 400 days of non-transfer transactions by merchant and direction, sorted by date.
func (s *Service) load(ctx context.Context, userID uuid.UUID) ([]group, error) {
	rows, err := s.db.Query(ctx, `SELECT id, merchant_key, merchant_enc, category_id, booked_on, amount_cents
		FROM transactions WHERE user_id = $1 AND transfer_id IS NULL AND booked_on > current_date - $2::int
		ORDER BY booked_on, id`, userID, lookbackDays)
	if err != nil {
		return nil, err
	}
	txns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (txnPoint, error) {
		var t txnPoint
		return t, row.Scan(&t.id, &t.merchantKey, &t.merchantEnc, &t.categoryID, &t.Date, &t.Amount)
	})
	if err != nil {
		return nil, err
	}
	byKey := map[string]*group{}
	var order []string
	for _, t := range txns {
		sign := "out"
		if t.Amount > 0 {
			sign = "in"
		}
		k := string(t.merchantKey) + sign
		if byKey[k] == nil {
			byKey[k] = &group{key: string(t.merchantKey), sign: sign}
			order = append(order, k)
		}
		byKey[k].txns = append(byKey[k].txns, t)
	}
	groups := make([]group, len(order))
	for i, k := range order {
		groups[i] = *byKey[k]
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].key < groups[j].key })
	return groups, nil
}

func (s *Service) subscriptionCategories(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT c.id FROM categories c LEFT JOIN categories p ON p.id = c.parent_id
		WHERE c.user_id = $1 AND (c.key = ANY($2) OR p.key = ANY($2))`, userID, subscriptionKeys)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	set := map[uuid.UUID]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set, err
}

func upsert(ctx context.Context, tx pgx.Tx, userID uuid.UUID, g group, s Series, subs map[uuid.UUID]bool) (uuid.UUID, error) {
	last := g.txns[len(g.txns)-1]
	subscription := s.Amount < 0 && (last.categoryID != nil && subs[*last.categoryID] ||
		last.categoryID == nil && s.LooksLikeSubscription())
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO recurring (id, user_id, merchant_key, display_enc, sign, cadence, amount_cents,
		previous_amount_cents, price_changed_on, avg_cents, occurrences, last_date, next_date, category_id, subscription)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (user_id, merchant_key, sign) DO UPDATE SET display_enc = EXCLUDED.display_enc,
		  cadence = EXCLUDED.cadence, amount_cents = EXCLUDED.amount_cents,
		  previous_amount_cents = EXCLUDED.previous_amount_cents, price_changed_on = EXCLUDED.price_changed_on,
		  avg_cents = EXCLUDED.avg_cents, occurrences = EXCLUDED.occurrences, last_date = EXCLUDED.last_date,
		  next_date = EXCLUDED.next_date, category_id = EXCLUDED.category_id, subscription = EXCLUDED.subscription,
		  updated_at = now()
		RETURNING id`, uuid.Must(uuid.NewV7()), userID, []byte(g.key), last.merchantEnc, g.sign, s.Cadence, s.Amount,
		s.PreviousAmount, s.PriceChangedOn, s.Average, s.Occurrences, s.LastDate, s.NextDate, last.categoryID,
		subscription).Scan(&id)
	if err != nil {
		return id, err
	}
	ids := make([]uuid.UUID, len(g.txns))
	for i, t := range g.txns {
		ids[i] = t.id
	}
	_, err = tx.Exec(ctx, `UPDATE transactions SET recurring_id = $1 WHERE id = ANY($2)`, id, ids)
	return id, err
}

// now is the reference date for "ended"; a variable so the meaning is explicit at the call sites.
var now = time.Now
