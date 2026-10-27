-- User categorization rules ("CARREFOUR" → Courses). Patterns are encrypted like labels; matching happens in Go.

-- +goose Up
CREATE TABLE rules (
    id          uuid PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    pattern_enc bytea       NOT NULL,
    match       text        NOT NULL CHECK (match IN ('merchant', 'contains')),
    sign        text        NOT NULL DEFAULT 'any' CHECK (sign IN ('any', 'in', 'out')),
    category_id uuid        NOT NULL REFERENCES categories ON DELETE CASCADE,
    priority    int         NOT NULL DEFAULT 0,
    origin      text        NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'correction')),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX rules_user ON rules (user_id);

-- +goose Down
DROP TABLE rules;
