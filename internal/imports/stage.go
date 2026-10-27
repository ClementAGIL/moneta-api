// Staging: parsed rows get their dedup key and status, and are stored in import_rows until commit.
package imports

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/dedup"
	"github.com/ClementAGIL/moneta-api/internal/importer"
	"github.com/ClementAGIL/moneta-api/internal/labels"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const maxRawLabel = 200

type staged struct {
	index      int
	date       *time.Time
	amount     int64
	labelEnc   []byte
	key        []byte
	occurrence int
	status     string
	match      *uuid.UUID
	err        string
}

// DedupKey is the blind index identifying "the same transaction" within an account.
func DedupKey(keys *vault.UserKeys, date time.Time, amount int64, label string) []byte {
	return keys.Index(fmt.Sprintf("%s|%d|%s", date.Format(dateLayout), amount, labels.Normalize(label)))
}

func (s *Service) stage(ctx context.Context, keys *vault.UserKeys, accountID uuid.UUID, grid importer.Grid,
	parsed []importer.Row) ([]staged, error) {
	var rows []staged
	var valid []dedup.Row
	seen := map[string]int{}
	for _, p := range parsed {
		if p.Err != "" {
			raw := strings.Join(grid.Rows[p.Index], " | ")
			if r := []rune(raw); len(r) > maxRawLabel {
				raw = string(r[:maxRawLabel])
			}
			rows = append(rows, staged{index: p.Index, labelEnc: keys.Seal(raw), status: "invalid", err: p.Err})
			continue
		}
		key := DedupKey(keys, p.Date, p.Amount, p.Label)
		seen[string(key)]++
		date := p.Date
		rows = append(rows, staged{index: p.Index, date: &date, amount: p.Amount, labelEnc: keys.Seal(p.Label),
			key: key, occurrence: seen[string(key)]})
		valid = append(valid, dedup.Row{Date: p.Date, Amount: p.Amount, Key: string(key), Occurrence: seen[string(key)]})
	}
	existing, err := s.existing(ctx, accountID, valid)
	if err != nil {
		return nil, err
	}
	results := dedup.Classify(valid, existing)
	next := 0
	for i := range rows {
		if rows[i].status == "invalid" {
			continue
		}
		rows[i].status, rows[i].match = results[next].Status, results[next].Match
		next++
	}
	return rows, nil
}

// existing loads the account's transactions around the rows' period.
func (s *Service) existing(ctx context.Context, accountID uuid.UUID, rows []dedup.Row) ([]dedup.Existing, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	start, end := rows[0].Date, rows[0].Date
	for _, r := range rows {
		start, end = minTime(start, r.Date), maxTime(end, r.Date)
	}
	dbRows, err := s.db.Query(ctx, `SELECT id, booked_on, amount_cents, dedup_key FROM transactions
		WHERE account_id = $1 AND booked_on BETWEEN $2::date - 3 AND $3::date + 3`, accountID, start, end)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(dbRows, func(row pgx.CollectableRow) (dedup.Existing, error) {
		var e dedup.Existing
		var key []byte
		err := row.Scan(&e.ID, &e.Date, &e.Amount, &key)
		e.Key = string(key)
		return e, err
	})
}

// saveStaged replaces the import's staged rows and counters.
func saveStaged(ctx context.Context, tx pgx.Tx, id, accountID, profileID uuid.UUID, rows []staged) error {
	if _, err := tx.Exec(ctx, `DELETE FROM import_rows WHERE import_id = $1`, id); err != nil {
		return err
	}
	counts := map[string]int{}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"import_rows"},
		[]string{"import_id", "row_index", "booked_on", "amount_cents", "label_enc", "dedup_key", "occurrence", "status",
			"match_txn_id", "error"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			r := rows[i]
			counts[r.status]++
			return []any{id, r.index, r.date, r.amount, r.labelEnc, r.key, r.occurrence, r.status, r.match,
				nullable(r.err)}, nil
		}))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE imports SET status = 'mapped', account_id = $2, profile_id = $3, rows_new = $4,
		rows_duplicate = $5, rows_review = $6, rows_invalid = $7,
		period_start = (SELECT min(booked_on) FROM import_rows WHERE import_id = $1),
		period_end = (SELECT max(booked_on) FROM import_rows WHERE import_id = $1)
		WHERE id = $1`, id, accountID, profileID, counts["new"], counts["duplicate"], counts["review"], counts["invalid"])
	return err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
