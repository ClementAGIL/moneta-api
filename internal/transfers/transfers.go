// Package transfers pairs movements between the user's own accounts (money out of one, the same amount into another
// within a few days) so they count neither as spending nor as income.
package transfers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
)

const maxDays = 3

type movement struct {
	id      uuid.UUID
	account uuid.UUID
	date    time.Time
	amount  int64
}

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Detect pairs unambiguous transfers among unpaired, not hand-categorized transactions. Returns the pairs made.
func (s *Service) Detect(ctx context.Context, userID uuid.UUID) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT id, account_id, booked_on, amount_cents FROM transactions
		WHERE user_id = $1 AND transfer_id IS NULL AND coalesce(category_source, '') <> 'user'`, userID)
	if err != nil {
		return 0, err
	}
	moves, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (movement, error) {
		var m movement
		return m, row.Scan(&m.id, &m.account, &m.date, &m.amount)
	})
	if err != nil {
		return 0, err
	}
	pairs := match(moves)
	for _, p := range pairs {
		if err := s.link(ctx, userID, p[0], p[1]); err != nil {
			return 0, err
		}
	}
	return len(pairs), nil
}

// match keeps only one-to-one candidates: an outgoing movement with exactly one possible incoming twin that itself
// has exactly one possible outgoing twin. Ambiguous cases are left to the user.
func match(moves []movement) [][2]uuid.UUID {
	candidates := map[uuid.UUID][]uuid.UUID{}
	for _, out := range moves {
		if out.amount >= 0 {
			continue
		}
		for _, in := range moves {
			if in.amount == -out.amount && in.account != out.account && closeDates(in.date, out.date) {
				candidates[out.id] = append(candidates[out.id], in.id)
				candidates[in.id] = append(candidates[in.id], out.id)
			}
		}
	}
	var pairs [][2]uuid.UUID
	for _, out := range moves {
		ins := candidates[out.id]
		if out.amount < 0 && len(ins) == 1 && len(candidates[ins[0]]) == 1 {
			pairs = append(pairs, [2]uuid.UUID{out.id, ins[0]})
		}
	}
	return pairs
}

func closeDates(a, b time.Time) bool {
	days := a.Sub(b).Hours() / 24
	return days >= -maxDays && days <= maxDays
}

// Pair links two transactions chosen by the user (they must be opposite amounts in different accounts).
func (s *Service) Pair(ctx context.Context, userID, a, b uuid.UUID) error {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT count(*) = 2 AND sum(amount_cents) = 0 AND count(DISTINCT account_id) = 2
		FROM transactions WHERE user_id = $1 AND id IN ($2, $3)`, userID, a, b).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.Invalid("id", "a transfer needs two opposite amounts in two different accounts")
	}
	if err := s.Unpair(ctx, userID, a); err != nil {
		return err
	}
	if err := s.Unpair(ctx, userID, b); err != nil {
		return err
	}
	return s.link(ctx, userID, a, b)
}

// Unpair removes the transfer link of a transaction and its twin; both go back to automatic categorization.
func (s *Service) Unpair(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE transactions SET transfer_id = NULL, category_id = NULL, category_source = NULL,
		updated_at = now()
		WHERE user_id = $1 AND transfer_id = (SELECT transfer_id FROM transactions WHERE id = $2 AND user_id = $1)`,
		userID, id)
	return err
}

func (s *Service) link(ctx context.Context, userID, a, b uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE transactions SET transfer_id = $4, category_source = 'transfer', confidence = NULL,
		category_id = (SELECT id FROM categories WHERE user_id = $1 AND key = 'transfers'), updated_at = now()
		WHERE user_id = $1 AND id IN ($2, $3)`, userID, a, b, uuid.Must(uuid.NewV7()))
	return err
}
