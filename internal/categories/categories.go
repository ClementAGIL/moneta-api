// Package categories stores each user's category tree (two levels: parent and children).
package categories

import (
	"context"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/db"
)

const maxNameLen = 60

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type Category struct {
	ID                uuid.UUID  `json:"id"`
	ParentID          *uuid.UUID `json:"parentId"`
	Key               *string    `json:"key"`
	Name              string     `json:"name"`
	Kind              string     `json:"kind"`
	Icon              string     `json:"icon"`
	Color             string     `json:"color"`
	ExcludedFromStats bool       `json:"excludedFromStats"`
}

// Input is the writable part of a category. Kind is inherited from the parent when there is one.
type Input struct {
	ParentID          *uuid.UUID `json:"parentId"`
	Name              string     `json:"name"`
	Kind              string     `json:"kind"`
	Icon              string     `json:"icon"`
	Color             string     `json:"color"`
	ExcludedFromStats bool       `json:"excludedFromStats"`
}

func (in *Input) validate() error {
	name, err := db.CleanName("name", in.Name, maxNameLen)
	if err != nil {
		return err
	}
	in.Name = name
	if in.Kind != "expense" && in.Kind != "income" && in.Kind != "transfer" {
		return apperr.Invalid("kind", "must be expense, income or transfer")
	}
	if !colorPattern.MatchString(in.Color) {
		return apperr.Invalid("color", "must look like #1F6F5C")
	}
	if in.Icon == "" {
		in.Icon = "tag"
	}
	return nil
}

type Store struct{ q db.Querier }

func NewStore(q db.Querier) *Store { return &Store{q: q} }

// Seed copies the default tree for a new user (run inside the sign-up transaction).
func Seed(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	batch := &pgx.Batch{}
	for i, parent := range defaults {
		parentID := uuid.Must(uuid.NewV7())
		queueSeed(batch, userID, parentID, nil, parent, i)
		for j, child := range parent.children {
			queueSeed(batch, userID, uuid.Must(uuid.NewV7()), &parentID, child, j)
		}
	}
	return tx.SendBatch(ctx, batch).Close()
}

func queueSeed(b *pgx.Batch, userID, id uuid.UUID, parentID *uuid.UUID, s seed, position int) {
	b.Queue(`INSERT INTO categories (id, user_id, parent_id, key, name, kind, icon, color, excluded_from_stats, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, userID, parentID, s.key, s.name, s.kind, s.icon, s.color, s.excluded, position)
}
