// Package imports runs the import flow: upload (grid stored encrypted) → mapping (parse + dedup into staged rows)
// → review → commit (transactions) or cancel/undo.
package imports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/importer"
	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	previewRows = 30
	maxFileName = 200
	dateLayout  = "2006-01-02"
)

// KeySource returns a user's encryption keys (auth.Service implements it).
type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

// Hook runs after a commit, e.g. to queue categorization (receives the import id).
type Hook func(ctx context.Context, userID, importID uuid.UUID) error

type Service struct {
	db       *pgxpool.Pool
	keys     KeySource
	onCommit Hook
}

func NewService(db *pgxpool.Pool, keys KeySource, onCommit Hook) *Service {
	return &Service{db: db, keys: keys, onCommit: onCommit}
}

// Preview is the upload response: the raw first rows and the suggested mapping for the wizard.
type Preview struct {
	ID          uuid.UUID        `json:"id"`
	FileName    string           `json:"fileName"`
	Kind        string           `json:"kind"`
	Encoding    string           `json:"encoding,omitempty"`
	Delimiter   string           `json:"delimiter,omitempty"`
	ColumnCount int              `json:"columnCount"`
	RowCount    int              `json:"rowCount"`
	Rows        [][]string       `json:"rows"`
	Mapping     importer.Mapping `json:"mapping"`
	DateFormats []string         `json:"dateFormats"`
	ProfileID   *uuid.UUID       `json:"profileId"` // set when the format was recognized
	AccountID   *uuid.UUID       `json:"accountId"` // account used last time with this format
}

// Summary describes an import and, once mapped, its rows.
type Summary struct {
	ID          uuid.UUID   `json:"id"`
	Status      string      `json:"status"`
	FileName    string      `json:"fileName"`
	AccountID   *uuid.UUID  `json:"accountId"`
	ProfileID   *uuid.UUID  `json:"profileId"`
	Total       int         `json:"rowsTotal"`
	New         int         `json:"rowsNew"`
	Duplicate   int         `json:"rowsDuplicate"`
	Review      int         `json:"rowsReview"`
	Invalid     int         `json:"rowsInvalid"`
	PeriodStart *string     `json:"periodStart"`
	PeriodEnd   *string     `json:"periodEnd"`
	CreatedAt   time.Time   `json:"createdAt"`
	CommittedAt *time.Time  `json:"committedAt"`
	Rows        []StagedRow `json:"rows,omitempty"`
}

// StagedRow is a parsed row waiting for commit.
type StagedRow struct {
	Index  int    `json:"index"`
	Date   string `json:"date,omitempty"`
	Amount int64  `json:"amountCents"`
	Label  string `json:"label"`
	Status string `json:"status"` // new, duplicate, review, invalid
	Error  string `json:"error,omitempty"`
	Match  *Match `json:"match,omitempty"`
}

// Match is the existing transaction a review row may duplicate.
type Match struct {
	ID     uuid.UUID `json:"id"`
	Date   string    `json:"date"`
	Amount int64     `json:"amountCents"`
	Label  string    `json:"label"`
}

func columnCount(rows [][]string) int {
	n := 0
	for _, r := range rows {
		n = max(n, len(r))
	}
	return n
}
