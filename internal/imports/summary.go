// Reading imports back: one import with its staged rows, or the history.
package imports

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	historyLimit  = 100
	selectSummary = `SELECT id, status, file_name_enc, account_id, profile_id, rows_total, rows_new, rows_duplicate,
		rows_review, rows_invalid, period_start::text, period_end::text, created_at, committed_at FROM imports`
)

// Get returns an import with its staged rows (present until commit).
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Summary, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	rows, err := s.db.Query(ctx, selectSummary+` WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return Summary{}, err
	}
	sum, err := pgx.CollectExactlyOneRow(rows, scanSummary(keys))
	if errors.Is(err, pgx.ErrNoRows) {
		return Summary{}, apperr.NotFound("import")
	}
	if err != nil {
		return Summary{}, err
	}
	sum.Rows, err = s.stagedRows(ctx, keys, id)
	return sum, err
}

// List returns the latest imports, newest first, without rows.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Summary, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, selectSummary+` WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, historyLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanSummary(keys))
}

func scanSummary(keys *vault.UserKeys) pgx.RowToFunc[Summary] {
	return func(row pgx.CollectableRow) (Summary, error) {
		var s Summary
		var nameEnc []byte
		err := row.Scan(&s.ID, &s.Status, &nameEnc, &s.AccountID, &s.ProfileID, &s.Total, &s.New, &s.Duplicate,
			&s.Review, &s.Invalid, &s.PeriodStart, &s.PeriodEnd, &s.CreatedAt, &s.CommittedAt)
		if err != nil {
			return s, err
		}
		s.FileName, err = keys.Open(nameEnc)
		return s, err
	}
}

func (s *Service) stagedRows(ctx context.Context, keys *vault.UserKeys, id uuid.UUID) ([]StagedRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.row_index, coalesce(r.booked_on::text, ''), coalesce(r.amount_cents, 0), r.label_enc, r.status,
		       coalesce(r.error, ''), t.id, t.booked_on::text, t.amount_cents, t.label_enc
		FROM import_rows r LEFT JOIN transactions t ON t.id = r.match_txn_id
		WHERE r.import_id = $1 ORDER BY r.row_index`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (StagedRow, error) {
		var r StagedRow
		var labelEnc, matchLabelEnc []byte
		var matchID *uuid.UUID
		var matchDate *string
		var matchAmount *int64
		err := row.Scan(&r.Index, &r.Date, &r.Amount, &labelEnc, &r.Status, &r.Error, &matchID, &matchDate,
			&matchAmount, &matchLabelEnc)
		if err != nil {
			return r, err
		}
		if r.Label, err = keys.Open(labelEnc); err != nil {
			return r, err
		}
		if matchID != nil {
			r.Match = &Match{ID: *matchID, Date: *matchDate, Amount: *matchAmount}
			r.Match.Label, err = keys.Open(matchLabelEnc)
		}
		return r, err
	})
}
