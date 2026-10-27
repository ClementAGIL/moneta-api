// Upload and mapping steps.
package imports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/accounts"
	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/importer"
)

// MappingRequest is the wizard's answer: target account and column mapping.
type MappingRequest struct {
	AccountID uuid.UUID        `json:"accountId"`
	Mapping   importer.Mapping `json:"mapping"`
}

// Upload reads the file, stores its cells encrypted (never the file itself) and suggests a mapping.
func (s *Service) Upload(ctx context.Context, userID uuid.UUID, fileName string, data []byte) (Preview, error) {
	grid, err := importer.ReadGrid(data)
	if err != nil {
		return Preview{}, apperr.Invalid("file", "%s", err.Error())
	}
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Preview{}, err
	}
	fileName = cleanFileName(fileName)
	mapping := importer.Detect(grid)
	fingerprint := importer.Fingerprint(grid, mapping.HeaderRow)
	p := Preview{ID: uuid.Must(uuid.NewV7()), FileName: fileName, Kind: grid.Kind, Encoding: grid.Encoding,
		Delimiter: grid.Delimiter, ColumnCount: columnCount(grid.Rows), RowCount: len(grid.Rows),
		Rows: grid.Rows[:min(len(grid.Rows), previewRows)], Mapping: mapping, DateFormats: importer.DateFormats}
	if err := s.applyProfile(ctx, userID, fingerprint, &p); err != nil {
		return Preview{}, err
	}
	gridJSON, err := json.Marshal(grid)
	if err != nil {
		return Preview{}, err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO imports (id, user_id, file_name_enc, file_kind, fingerprint, grid_enc, rows_total)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.ID, userID, keys.Seal(fileName), grid.Kind, fingerprint, keys.Seal(string(gridJSON)), len(grid.Rows))
	return p, err
}

// applyProfile replaces the guess with the saved mapping when this file layout was imported before.
func (s *Service) applyProfile(ctx context.Context, userID uuid.UUID, fingerprint string, p *Preview) error {
	var id uuid.UUID
	var mapping []byte
	var account *uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT id, mapping, last_account_id FROM import_profiles
		WHERE user_id = $1 AND fingerprint = $2`, userID, fingerprint).Scan(&id, &mapping, &account)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(mapping, &p.Mapping); err != nil {
		return fmt.Errorf("reading profile mapping: %w", err)
	}
	p.ProfileID, p.AccountID = &id, account
	return nil
}

// SetMapping parses the rows with the mapping, classifies them against the account, and saves the mapping as the
// profile for this file layout. It can be called again to change the mapping or the account.
func (s *Service) SetMapping(ctx context.Context, userID, id uuid.UUID, req MappingRequest) (Summary, error) {
	keys, err := s.keys.Keys(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	grid, err := s.loadGrid(ctx, userID, id, keys.Open)
	if err != nil {
		return Summary{}, err
	}
	if err := req.Mapping.Validate(columnCount(grid.Rows)); err != nil {
		return Summary{}, err
	}
	account, err := accounts.NewStore(s.db).AccountOf(ctx, userID, req.AccountID)
	if err != nil {
		return Summary{}, apperr.Invalid("accountId", "unknown account")
	}
	rows, err := s.stage(ctx, keys, req.AccountID, grid, req.Mapping.Parse(grid))
	if err != nil {
		return Summary{}, err
	}
	fingerprint := importer.Fingerprint(grid, req.Mapping.HeaderRow)
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		profileID, err := saveProfile(ctx, tx, userID, fingerprint, account, req.Mapping)
		if err != nil {
			return err
		}
		return saveStaged(ctx, tx, id, req.AccountID, profileID, rows)
	})
	if err != nil {
		return Summary{}, err
	}
	return s.Get(ctx, userID, id)
}

// loadGrid returns the cells of an import still open for mapping.
func (s *Service) loadGrid(ctx context.Context, userID, id uuid.UUID, open func([]byte) (string, error)) (importer.Grid, error) {
	var gridEnc []byte
	var status string
	err := s.db.QueryRow(ctx, `SELECT status, grid_enc FROM imports WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&status, &gridEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return importer.Grid{}, apperr.NotFound("import")
	}
	if err != nil {
		return importer.Grid{}, err
	}
	if status != "uploaded" && status != "mapped" {
		return importer.Grid{}, apperr.Conflict("this import is already %s", status)
	}
	text, err := open(gridEnc)
	if err != nil {
		return importer.Grid{}, err
	}
	var grid importer.Grid
	return grid, json.Unmarshal([]byte(text), &grid)
}

func saveProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID, fingerprint string, account accounts.Account,
	mapping importer.Mapping) (uuid.UUID, error) {
	body, err := json.Marshal(mapping)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO import_profiles (id, user_id, fingerprint, name, mapping, last_account_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, fingerprint) DO UPDATE
		SET mapping = EXCLUDED.mapping, last_account_id = EXCLUDED.last_account_id, updated_at = now()
		RETURNING id`, uuid.Must(uuid.NewV7()), userID, fingerprint, account.Name, body, account.ID).Scan(&id)
	return id, err
}

func cleanFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "import"
	}
	if r := []rune(name); len(r) > maxFileName {
		name = string(r[:maxFileName])
	}
	return name
}
