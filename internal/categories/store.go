// SQL for categories.
package categories

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/db"
)

const selectCategory = `SELECT id, parent_id, key, name, kind, icon, color, excluded_from_stats FROM categories`

// List returns parents first, each followed by its children, in display order.
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Category, error) {
	rows, err := s.q.Query(ctx, `
		SELECT c.id, c.parent_id, c.key, c.name, c.kind, c.icon, c.color, c.excluded_from_stats
		FROM categories c LEFT JOIN categories p ON p.id = c.parent_id
		WHERE c.user_id = $1
		ORDER BY coalesce(p.position, c.position), coalesce(c.parent_id, c.id), c.parent_id NULLS FIRST, c.position,
		         c.name`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanCategory)
}

// Get returns one of the user's categories.
func (s *Store) Get(ctx context.Context, userID, id uuid.UUID) (Category, error) {
	rows, err := s.q.Query(ctx, selectCategory+` WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return Category{}, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanCategory)
	return c, db.NotFoundIfNoRows(err, "category")
}

// ByKey maps default keys to the user's category ids.
func (s *Store) ByKey(ctx context.Context, userID uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := s.q.Query(ctx, `SELECT key, id FROM categories WHERE user_id = $1 AND key IS NOT NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]uuid.UUID{}
	for rows.Next() {
		var key string
		var id uuid.UUID
		if err := rows.Scan(&key, &id); err != nil {
			return nil, err
		}
		ids[key] = id
	}
	return ids, rows.Err()
}

func (s *Store) Create(ctx context.Context, userID uuid.UUID, in Input) (Category, error) {
	if err := s.prepare(ctx, userID, nil, &in); err != nil {
		return Category{}, err
	}
	id := uuid.Must(uuid.NewV7())
	_, err := s.q.Exec(ctx, `INSERT INTO categories (id, user_id, parent_id, name, kind, icon, color, excluded_from_stats,
		position) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1000)`,
		id, userID, in.ParentID, in.Name, in.Kind, in.Icon, in.Color, in.ExcludedFromStats)
	if err != nil {
		return Category{}, err
	}
	return s.Get(ctx, userID, id)
}

func (s *Store) Update(ctx context.Context, userID, id uuid.UUID, in Input) (Category, error) {
	if err := s.prepare(ctx, userID, &id, &in); err != nil {
		return Category{}, err
	}
	tag, err := s.q.Exec(ctx, `UPDATE categories SET parent_id = $3, name = $4, kind = $5, icon = $6, color = $7,
		excluded_from_stats = $8 WHERE id = $2 AND user_id = $1`,
		userID, id, in.ParentID, in.Name, in.Kind, in.Icon, in.Color, in.ExcludedFromStats)
	if err := db.Affected(tag, err, "category"); err != nil {
		return Category{}, err
	}
	return s.Get(ctx, userID, id)
}

// Delete removes a category and its children; their transactions become uncategorized (FK ON DELETE SET NULL).
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.q.Exec(ctx, `DELETE FROM categories WHERE id = $2 AND user_id = $1 AND key IS DISTINCT FROM 'transfers'`,
		userID, id)
	return db.Affected(tag, err, "category")
}

// prepare validates the input and enforces the two-level tree: the parent must be a top-level category of the user.
func (s *Store) prepare(ctx context.Context, userID uuid.UUID, self *uuid.UUID, in *Input) error {
	if in.ParentID != nil {
		parent, err := s.Get(ctx, userID, *in.ParentID)
		if err != nil {
			return apperr.Invalid("parentId", "unknown parent category")
		}
		if parent.ParentID != nil || (self != nil && parent.ID == *self) {
			return apperr.Invalid("parentId", "categories have two levels at most")
		}
		in.Kind = parent.Kind
		if self != nil && s.hasChildren(ctx, *self) {
			return apperr.Invalid("parentId", "a category with subcategories cannot become a subcategory")
		}
	}
	return in.validate()
}

func (s *Store) hasChildren(ctx context.Context, id uuid.UUID) bool {
	var exists bool
	_ = s.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE parent_id = $1)`, id).Scan(&exists)
	return exists
}

func scanCategory(row pgx.CollectableRow) (Category, error) {
	var c Category
	return c, row.Scan(&c.ID, &c.ParentID, &c.Key, &c.Name, &c.Kind, &c.Icon, &c.Color, &c.ExcludedFromStats)
}
