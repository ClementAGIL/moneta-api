// Rules store: CRUD on encrypted rule patterns, and the rule created from a correction.
package categorize

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/db"
	"github.com/ClementAGIL/moneta-api/internal/jobs"
	"github.com/ClementAGIL/moneta-api/internal/labels"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const maxPatternLen = 100

// RuleInput is the writable part of a rule.
type RuleInput struct {
	Pattern    string    `json:"pattern"`
	Match      string    `json:"match"`
	Sign       string    `json:"sign"`
	CategoryID uuid.UUID `json:"categoryId"`
	Priority   int       `json:"priority"`
}

func (in *RuleInput) validate() error {
	pattern, err := db.CleanName("pattern", in.Pattern, maxPatternLen)
	if err != nil {
		return err
	}
	if labels.Normalize(pattern) == "" {
		return apperr.Invalid("pattern", "needs letters or digits")
	}
	in.Pattern = pattern
	if in.Match != "merchant" && in.Match != "contains" {
		return apperr.Invalid("match", "must be merchant or contains")
	}
	if in.Sign == "" {
		in.Sign = "any"
	}
	if in.Sign != "any" && in.Sign != "in" && in.Sign != "out" {
		return apperr.Invalid("sign", "must be any, in or out")
	}
	return nil
}

// Rules returns the user's rules, decrypted.
func (s *Service) Rules(ctx context.Context, userID uuid.UUID, keys *vault.UserKeys) ([]Rule, error) {
	rows, err := s.db.Query(ctx, `SELECT id, pattern_enc, match, sign, category_id, priority, origin FROM rules
		WHERE user_id = $1 ORDER BY priority DESC, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Rule, error) {
		var r Rule
		var patternEnc []byte
		if err := row.Scan(&r.ID, &patternEnc, &r.Match, &r.Sign, &r.CategoryID, &r.Priority, &r.Origin); err != nil {
			return r, err
		}
		var err error
		r.Pattern, err = keys.Open(patternEnc)
		return r, err
	})
}

// SaveRule creates (id nil) or updates a rule, then queues a recategorization.
func (s *Service) SaveRule(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in RuleInput, origin string) (uuid.UUID, error) {
	if err := in.validate(); err != nil {
		return uuid.Nil, err
	}
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return uuid.Nil, err
	}
	var tag interface{ RowsAffected() int64 }
	if id == nil {
		newID := uuid.Must(uuid.NewV7())
		id = &newID
		tag, err = s.db.Exec(ctx, `INSERT INTO rules (id, user_id, pattern_enc, match, sign, category_id, priority, origin)
			SELECT $1, $2, $3, $4, $5, c.id, $7, $8 FROM categories c WHERE c.id = $6 AND c.user_id = $2`,
			*id, userID, keys.Seal(in.Pattern), in.Match, in.Sign, in.CategoryID, in.Priority, origin)
	} else {
		tag, err = s.db.Exec(ctx, `UPDATE rules SET pattern_enc = $3, match = $4, sign = $5, category_id = c.id,
			priority = $7 FROM categories c WHERE rules.id = $1 AND rules.user_id = $2 AND c.id = $6 AND c.user_id = $2`,
			*id, userID, keys.Seal(in.Pattern), in.Match, in.Sign, in.CategoryID, in.Priority)
	}
	if err != nil {
		return uuid.Nil, err
	}
	if tag.RowsAffected() == 0 {
		return uuid.Nil, apperr.NotFound("rule or category")
	}
	return *id, s.queueRecategorize(ctx, userID)
}

func (s *Service) DeleteRule(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM rules WHERE id = $1 AND user_id = $2`, id, userID)
	if err := db.Affected(tag, err, "rule"); err != nil {
		return err
	}
	return s.queueRecategorize(ctx, userID)
}

// Preview returns the user's transactions a rule would match, newest first.
func (s *Service) Preview(ctx context.Context, userID uuid.UUID, in RuleInput, limit int) ([]uuid.UUID, int, error) {
	if err := in.validate(); err != nil {
		return nil, 0, err
	}
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	rule := Rule{Pattern: in.Pattern, Match: in.Match, Sign: in.Sign}
	rows, err := s.db.Query(ctx, `SELECT id, label_enc, amount_cents FROM transactions WHERE user_id = $1
		ORDER BY booked_on DESC, id DESC`, userID)
	if err != nil {
		return nil, 0, err
	}
	var ids []uuid.UUID
	total := 0
	var id uuid.UUID
	var labelEnc []byte
	var amount int64
	_, err = pgx.ForEachRow(rows, []any{&id, &labelEnc, &amount}, func() error {
		label, err := keys.Open(labelEnc)
		if err == nil && Matches(rule, Txn{Label: label, Amount: amount}) {
			total++
			if len(ids) < limit {
				ids = append(ids, id)
			}
		}
		return err
	})
	return ids, total, err
}

// CorrectMerchant records "this merchant goes to that category" from a correction: it updates the user's existing
// merchant rule for the same merchant and direction, or creates one.
func (s *Service) CorrectMerchant(ctx context.Context, userID uuid.UUID, merchant string, amount int64,
	categoryID uuid.UUID) error {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return err
	}
	rules, err := s.Rules(ctx, userID, keys)
	if err != nil {
		return err
	}
	in := RuleInput{Pattern: merchant, Match: "merchant", Sign: "out", CategoryID: categoryID}
	if amount > 0 {
		in.Sign = "in"
	}
	for _, r := range rules {
		if r.Match == "merchant" && r.Sign == in.Sign && labels.Normalize(r.Pattern) == labels.Normalize(merchant) {
			in.Priority = r.Priority
			_, err := s.SaveRule(ctx, userID, &r.ID, in, "correction")
			return err
		}
	}
	_, err = s.SaveRule(ctx, userID, nil, in, "correction")
	return err
}

func (s *Service) queueRecategorize(ctx context.Context, userID uuid.UUID) error {
	return jobs.Enqueue(ctx, s.db, JobKind, &userID, struct{}{}, time.Time{})
}
