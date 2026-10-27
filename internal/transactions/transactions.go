// Package transactions lists, searches and edits the user's transactions (labels are decrypted here).
package transactions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/labels"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	defaultLimit = 50
	maxLimit     = 500
	scanPage     = 500 // rows decrypted per round when searching
	dateLayout   = "2006-01-02"
)

type Transaction struct {
	ID             uuid.UUID  `json:"id"`
	AccountID      uuid.UUID  `json:"accountId"`
	Date           string     `json:"date"`
	Amount         int64      `json:"amountCents"`
	Label          string     `json:"label"`
	Merchant       string     `json:"merchant"`
	CategoryID     *uuid.UUID `json:"categoryId"`
	CategorySource *string    `json:"categorySource"`
	Confidence     *float32   `json:"confidence"`
	Note           string     `json:"note,omitempty"`
	TransferID     *uuid.UUID `json:"transferId"`
	RecurringID    *uuid.UUID `json:"recurringId"`
}

// Filter narrows a listing. Category includes its subcategories.
type Filter struct {
	AccountID     *uuid.UUID
	CategoryID    *uuid.UUID
	Uncategorized bool
	From, To      *time.Time
	Query         string
	Limit         int
	Cursor        string
}

// Page is a listing page; Next is empty on the last page.
type Page struct {
	Items []Transaction `json:"items"`
	Next  string        `json:"next"`
}

type cursor struct {
	Date string    `json:"d"`
	ID   uuid.UUID `json:"i"`
}

type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

type Service struct {
	db   *pgxpool.Pool
	keys KeySource
}

func NewService(db *pgxpool.Pool, keys KeySource) *Service { return &Service{db: db, keys: keys} }

const selectTxn = `SELECT t.id, t.account_id, t.booked_on::text, t.amount_cents, t.label_enc, t.merchant_enc,
	t.category_id, t.category_source, t.confidence, t.note_enc, t.transfer_id, t.recurring_id FROM transactions t`

// List returns transactions newest first. A text query is matched in Go on decrypted labels, page by page.
func (s *Service) List(ctx context.Context, userID uuid.UUID, f Filter) (Page, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Page{}, err
	}
	f.Limit = clampLimit(f.Limit)
	after, err := decodeCursor(f.Cursor)
	if err != nil {
		return Page{}, err
	}
	query := labels.Normalize(f.Query)
	var items []Transaction
	for {
		batch, err := s.page(ctx, userID, keys, f, after)
		if err != nil {
			return Page{}, err
		}
		for _, t := range batch {
			after = &cursor{Date: t.Date, ID: t.ID}
			if query == "" || strings.Contains(labels.Normalize(t.Label), query) {
				items = append(items, t)
				if len(items) == f.Limit {
					return Page{Items: items, Next: encodeCursor(after)}, nil
				}
			}
		}
		if len(batch) < pageSize(f, query) {
			return Page{Items: nonNil(items)}, nil
		}
	}
}

// Get returns one transaction of the user.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Transaction, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Transaction{}, err
	}
	rows, err := s.db.Query(ctx, selectTxn+` WHERE t.user_id = $1 AND t.id = $2`, userID, id)
	if err != nil {
		return Transaction{}, err
	}
	items, err := collect(rows, keys)
	if err != nil {
		return Transaction{}, err
	}
	if len(items) == 0 {
		return Transaction{}, apperr.NotFound("transaction")
	}
	return items[0], nil
}

func (s *Service) page(ctx context.Context, userID uuid.UUID, keys *vault.UserKeys, f Filter, after *cursor) ([]Transaction, error) {
	var afterDate *string
	var afterID *uuid.UUID
	if after != nil {
		afterDate, afterID = &after.Date, &after.ID
	}
	rows, err := s.db.Query(ctx, selectTxn+`
		WHERE t.user_id = $1
		  AND ($2::uuid IS NULL OR t.account_id = $2)
		  AND ($3::uuid IS NULL OR t.category_id IN (SELECT id FROM categories WHERE id = $3 OR parent_id = $3))
		  AND (NOT $4 OR t.category_id IS NULL)
		  AND ($5::date IS NULL OR t.booked_on >= $5) AND ($6::date IS NULL OR t.booked_on <= $6)
		  AND ($7::date IS NULL OR (t.booked_on, t.id) < ($7::date, $8::uuid))
		ORDER BY t.booked_on DESC, t.id DESC LIMIT $9`,
		userID, f.AccountID, f.CategoryID, f.Uncategorized, f.From, f.To, afterDate, afterID,
		pageSize(f, labels.Normalize(f.Query)))
	if err != nil {
		return nil, err
	}
	return collect(rows, keys)
}

func collect(rows pgx.Rows, keys *vault.UserKeys) ([]Transaction, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Transaction, error) {
		var t Transaction
		var labelEnc, merchantEnc, noteEnc []byte
		err := row.Scan(&t.ID, &t.AccountID, &t.Date, &t.Amount, &labelEnc, &merchantEnc, &t.CategoryID,
			&t.CategorySource, &t.Confidence, &noteEnc, &t.TransferID, &t.RecurringID)
		if err != nil {
			return t, err
		}
		if t.Label, err = keys.Open(labelEnc); err != nil {
			return t, err
		}
		if t.Merchant, err = keys.Open(merchantEnc); err != nil {
			return t, err
		}
		if noteEnc != nil {
			t.Note, err = keys.Open(noteEnc)
		}
		return t, err
	})
}

// pageSize fetches exactly one page without a query, bigger rounds with one (most rows get filtered out).
func pageSize(f Filter, query string) int {
	if query == "" {
		return f.Limit
	}
	return scanPage
}

func clampLimit(n int) int {
	if n <= 0 {
		return defaultLimit
	}
	return min(n, maxLimit)
}

func encodeCursor(c *cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*cursor, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	var c cursor
	if err != nil || json.Unmarshal(b, &c) != nil {
		return nil, apperr.Invalid("cursor", "invalid cursor")
	}
	if _, err := time.Parse(dateLayout, c.Date); err != nil {
		return nil, apperr.Invalid("cursor", "invalid cursor")
	}
	return &c, nil
}

func nonNil(items []Transaction) []Transaction {
	if items == nil {
		return []Transaction{}
	}
	return items
}
