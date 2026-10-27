// Training, storing (encrypted) and loading a user's model, and its status for the API.
package ml

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	MinSamples    = 200  // below this, the rules alone are better
	minAccuracy   = 0.75 // holdout accuracy needed to use the model
	minConfidence = 0.6  // below this, a transaction stays uncategorized
	JobKind       = "model.retrain"
)

type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

// Status is what GET /model returns.
type Status struct {
	Samples     int        `json:"samples"`
	MinSamples  int        `json:"minSamples"`
	TrainedAt   *time.Time `json:"trainedAt"`
	Tested      int        `json:"tested"`
	Accuracy    float64    `json:"accuracy"`
	UserTested  int        `json:"userTested"`
	ModelOnUser float64    `json:"modelOnUser"`
	RulesOnUser float64    `json:"rulesOnUser"`
	Active      bool       `json:"active"`
}

type Service struct {
	db         *pgxpool.Pool
	keys       KeySource
	categorize *categorize.Service
}

func NewService(db *pgxpool.Pool, keys KeySource, c *categorize.Service) *Service {
	return &Service{db: db, keys: keys, categorize: c}
}

// Retrain evaluates a model on the holdout, then trains it on every sample and stores it with its evaluation.
func (s *Service) Retrain(ctx context.Context, userID uuid.UUID) error {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return err
	}
	samples, err := s.samples(ctx, userID, keys)
	if err != nil {
		return err
	}
	e := Evaluate(samples)
	all := make([]Sample, len(samples))
	for i, sm := range samples {
		all[i] = sm.Sample
	}
	var state bytes.Buffer
	if err := gob.NewEncoder(&state).Encode(Train(all)); err != nil {
		return err
	}
	active := len(samples) >= MinSamples && e.Accuracy >= minAccuracy
	_, err = s.db.Exec(ctx, `INSERT INTO models (user_id, samples, tested, accuracy, user_tested, model_on_user,
		rules_on_user, active, state_enc) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id) DO UPDATE SET trained_at = now(), samples = $2, tested = $3, accuracy = $4,
		  user_tested = $5, model_on_user = $6, rules_on_user = $7, active = $8, state_enc = $9`,
		userID, len(samples), e.Tested, e.Accuracy, e.UserTested, e.ModelOnUser, e.RulesOnUser, active,
		keys.Seal(state.String()))
	return err
}

// samples reads the categorized transactions (oldest first) with what the rules alone would say.
func (s *Service) samples(ctx context.Context, userID uuid.UUID, keys *vault.UserKeys) ([]Labeled, error) {
	rules, err := s.categorize.Engine(ctx, userID, keys, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT label_enc, amount_cents, category_id::text, category_source = 'user'
		FROM transactions WHERE user_id = $1 AND category_id IS NOT NULL AND transfer_id IS NULL
		  AND category_source IN ('user', 'rule', 'builtin') ORDER BY booked_on, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Labeled, error) {
		var l Labeled
		var labelEnc []byte
		var amount int64
		if err := row.Scan(&labelEnc, &amount, &l.Class, &l.ByUser); err != nil {
			return l, err
		}
		label, err := keys.Open(labelEnc)
		l.Features = Features(label, amount)
		if d := rules.Categorize(categorize.Txn{Label: label, Amount: amount}); d.CategoryID != nil {
			l.RulesClass = d.CategoryID.String()
		}
		return l, err
	})
}

// Predictor returns the user's model when it is active, else nil (categorize.ModelSource).
func (s *Service) Predictor(ctx context.Context, userID uuid.UUID) (categorize.Predictor, error) {
	var stateEnc []byte
	err := s.db.QueryRow(ctx, `SELECT state_enc FROM models WHERE user_id = $1 AND active`, userID).Scan(&stateEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return nil, err
	}
	state, err := keys.Open(stateEnc)
	if err != nil {
		return nil, err
	}
	var m Model
	if err := gob.NewDecoder(bytes.NewBufferString(state)).Decode(&m); err != nil {
		return nil, err
	}
	return predictor{&m}, nil
}

// Status reports the model's evaluation (zeros before the first training).
func (s *Service) Status(ctx context.Context, userID uuid.UUID) (Status, error) {
	st := Status{MinSamples: MinSamples}
	err := s.db.QueryRow(ctx, `SELECT samples, trained_at, tested, accuracy, user_tested, model_on_user, rules_on_user,
		active FROM models WHERE user_id = $1`, userID).Scan(&st.Samples, &st.TrainedAt, &st.Tested, &st.Accuracy,
		&st.UserTested, &st.ModelOnUser, &st.RulesOnUser, &st.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	return st, err
}

// predictor adapts Model to categorize.Predictor.
type predictor struct{ m *Model }

func (p predictor) Predict(label string, amount int64) (uuid.UUID, float32, bool) {
	class, probability := p.m.Predict(Features(label, amount))
	id, err := uuid.Parse(class)
	if err != nil || probability < minConfidence {
		return uuid.Nil, 0, false
	}
	return id, float32(probability), true
}

func (p predictor) Explain(label string, amount int64, class uuid.UUID) []string {
	return p.m.Explain(Features(label, amount), class.String())
}
