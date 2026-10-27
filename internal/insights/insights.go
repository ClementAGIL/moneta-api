// Package insights turns a month's numbers into short pieces of advice. Each rule (rules.go) is a function in a
// table; Generate runs them all and stores the result, keeping what the user dismissed.
package insights

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Insight is one piece of advice. Params hold ids and amounts; clients turn kind + params into a sentence.
type Insight struct {
	ID       uuid.UUID      `json:"id"`
	Kind     string         `json:"kind"`
	Period   string         `json:"period"` // YYYY-MM
	Severity string         `json:"severity"`
	Params   map[string]any `json:"params"`
	ref      string
}

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Generate recomputes the insights of the month starting at month: new ones are added, changed ones updated,
// stale ones (no longer true) deleted unless dismissed.
func (s *Service) Generate(ctx context.Context, userID uuid.UUID, month time.Time) error {
	var all []Insight
	for _, rule := range rules {
		found, err := rule(ctx, s.db, userID, month)
		if err != nil {
			return err
		}
		all = append(all, found...)
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		keys := make([]string, 0, len(all))
		for _, in := range all {
			params, err := json.Marshal(in.Params)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO insights (id, user_id, kind, period, ref, severity, params)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (user_id, kind, period, ref) DO UPDATE SET severity = EXCLUDED.severity,
				  params = EXCLUDED.params`, uuid.Must(uuid.NewV7()), userID, in.Kind, month, in.ref, in.Severity, params)
			if err != nil {
				return err
			}
			keys = append(keys, in.Kind+"/"+in.ref)
		}
		_, err := tx.Exec(ctx, `DELETE FROM insights WHERE user_id = $1 AND period = $2 AND dismissed_at IS NULL
			AND NOT (kind || '/' || ref = ANY($3))`, userID, month, keys)
		return err
	})
}

// List returns the user's insights not dismissed for the given months, most severe first.
func (s *Service) List(ctx context.Context, userID uuid.UUID, months []time.Time) ([]Insight, error) {
	rows, err := s.db.Query(ctx, `SELECT id, kind, to_char(period, 'YYYY-MM'), severity, params FROM insights
		WHERE user_id = $1 AND period = ANY($2) AND dismissed_at IS NULL
		ORDER BY array_position(ARRAY['alert', 'warn', 'good', 'info'], severity), period DESC, created_at`,
		userID, months)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Insight, error) {
		var in Insight
		return in, row.Scan(&in.ID, &in.Kind, &in.Period, &in.Severity, &in.Params)
	})
}

// Dismiss hides an insight for good (regenerating keeps it dismissed).
func (s *Service) Dismiss(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE insights SET dismissed_at = now() WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func newInsight(kind, severity, ref string, params map[string]any) Insight {
	return Insight{Kind: kind, Severity: severity, ref: ref, Params: params}
}
