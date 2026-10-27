// Running the engine over stored transactions, and the rules store.
package categorize

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/categories"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

// JobKind recategorizes all of a user's transactions (queued after rule changes).
const JobKind = "categorize.user"

// KeySource returns a user's encryption keys.
type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

// ModelSource returns the user's active classifier, or nil.
type ModelSource func(ctx context.Context, userID uuid.UUID) (Predictor, error)

type Service struct {
	db    *pgxpool.Pool
	keys  KeySource
	model ModelSource
}

func NewService(db *pgxpool.Pool, keys KeySource) *Service {
	return &Service{db: db, keys: keys}
}

// UseModel plugs the classifier in (set after construction: the ml package depends on this one).
func (s *Service) UseModel(model ModelSource) {
	s.model = model
}

// Engine builds the user's engine (rules, default category ids, and the model when withModel is set).
func (s *Service) Engine(ctx context.Context, userID uuid.UUID, keys *vault.UserKeys,
	withModel bool) (*Engine, error) {
	rules, err := s.Rules(ctx, userID, keys)
	if err != nil {
		return nil, err
	}
	byKey, err := categories.NewStore(s.db).ByKey(ctx, userID)
	if err != nil {
		return nil, err
	}
	var model Predictor
	if withModel && s.model != nil {
		if model, err = s.model(ctx, userID); err != nil {
			return nil, err
		}
	}
	return NewEngine(rules, byKey, model), nil
}

// Recategorize re-runs the engine on the user's transactions that were not set by hand or as transfers (only those
// of one import when importID is set).
func (s *Service) Recategorize(ctx context.Context, userID uuid.UUID, importID *uuid.UUID) error {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return err
	}
	engine, err := s.Engine(ctx, userID, keys, true)
	if err != nil {
		return err
	}
	return s.apply(ctx, userID, importID, keys, engine)
}

// apply reads the candidate rows and sends one UPDATE per row, with the decision computed in Go (labels are
// encrypted, so matching cannot happen in SQL).
func (s *Service) apply(ctx context.Context, userID uuid.UUID, importID *uuid.UUID, keys *vault.UserKeys,
	engine *Engine) error {
	batch := &pgx.Batch{}
	rows, err := s.db.Query(ctx, `SELECT id, label_enc, amount_cents, category_id, category_source FROM transactions
		WHERE user_id = $1 AND ($2::uuid IS NULL OR import_id = $2)
		  AND (category_source IS NULL OR category_source IN ('rule', 'builtin', 'model'))`, userID, importID)
	if err != nil {
		return err
	}
	var id uuid.UUID
	var labelEnc []byte
	var amount int64
	var current *uuid.UUID
	var source *string
	_, err = pgx.ForEachRow(rows, []any{&id, &labelEnc, &amount, &current, &source}, func() error {
		label, err := keys.Open(labelEnc)
		if err != nil {
			return err
		}
		d := engine.Categorize(Txn{Label: label, Amount: amount})
		if sameID(d.CategoryID, current) && sameString(d.Source, source) {
			return nil // most rows do not change: skip the write
		}
		batch.Queue(`UPDATE transactions SET category_id = $2, category_source = $3, confidence = $4, updated_at = now()
			WHERE id = $1`, id, d.CategoryID, d.Source, d.Confidence)
		return nil
	})
	if err != nil || batch.Len() == 0 {
		return err
	}
	return s.db.SendBatch(ctx, batch).Close()
}

// Explain says why transaction (label, amount) got its automatic category.
func (s *Service) Explain(ctx context.Context, userID uuid.UUID, label string,
	amount int64) (Decision, []string, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Decision{}, nil, err
	}
	engine, err := s.Engine(ctx, userID, keys, true)
	if err != nil {
		return Decision{}, nil, err
	}
	d, reasons := engine.Explain(Txn{Label: label, Amount: amount})
	return d, reasons, nil
}

func sameID(a, b *uuid.UUID) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func sameString(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
