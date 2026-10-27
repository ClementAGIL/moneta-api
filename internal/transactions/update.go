// Editing a transaction: category (a correction, optionally turned into a merchant rule) and note.
package transactions

import (
	"context"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
)

const maxNoteLen = 500

// Update is a partial edit: nil fields are left as they are; CategoryID "" clears the category.
type Update struct {
	CategoryID      *string `json:"categoryId"`
	Note            *string `json:"note"`
	ApplyToMerchant bool    `json:"applyToMerchant"` // also create a rule for this merchant
}

// RuleMaker creates a correction rule for a merchant (categorize.Service implements it via an adapter).
type RuleMaker func(ctx context.Context, userID uuid.UUID, merchant string, amount int64, categoryID uuid.UUID) error

// Edit applies u to transaction id. A category set by hand is marked "user" and never recategorized automatically.
func (s *Service) Edit(ctx context.Context, userID, id uuid.UUID, u Update, makeRule RuleMaker) (Transaction, error) {
	t, err := s.Get(ctx, userID, id)
	if err != nil {
		return Transaction{}, err
	}
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Transaction{}, err
	}
	if u.Note != nil {
		if len([]rune(*u.Note)) > maxNoteLen {
			return Transaction{}, apperr.Invalid("note", "at most %d characters", maxNoteLen)
		}
		var noteEnc []byte
		if *u.Note != "" {
			noteEnc = keys.Seal(*u.Note)
		}
		if _, err := s.db.Exec(ctx, `UPDATE transactions SET note_enc = $3, updated_at = now() WHERE id = $1 AND user_id = $2`,
			id, userID, noteEnc); err != nil {
			return Transaction{}, err
		}
	}
	if u.CategoryID != nil {
		if err := s.setCategory(ctx, userID, t, *u.CategoryID, u.ApplyToMerchant, makeRule); err != nil {
			return Transaction{}, err
		}
	}
	return s.Get(ctx, userID, id)
}

func (s *Service) setCategory(ctx context.Context, userID uuid.UUID, t Transaction, raw string, applyToMerchant bool,
	makeRule RuleMaker) error {
	var categoryID *uuid.UUID
	if raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return apperr.Invalid("categoryId", "unknown category")
		}
		categoryID = &id
	}
	tag, err := s.db.Exec(ctx, `UPDATE transactions SET category_id = $3, category_source = 'user', confidence = NULL,
		updated_at = now()
		WHERE id = $1 AND user_id = $2 AND ($3::uuid IS NULL OR EXISTS (
			SELECT 1 FROM categories WHERE id = $3 AND user_id = $2))`, t.ID, userID, categoryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.Invalid("categoryId", "unknown category")
	}
	if applyToMerchant && categoryID != nil && makeRule != nil {
		return makeRule(ctx, userID, t.Merchant, t.Amount, *categoryID)
	}
	return nil
}
