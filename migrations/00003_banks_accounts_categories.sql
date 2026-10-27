-- Banks and their accounts, and each user's category tree (seeded from Go defaults at sign-up).

-- +goose Up
CREATE TABLE banks (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    name       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE TABLE accounts (
    id                   uuid PRIMARY KEY,
    user_id              uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    bank_id              uuid        NOT NULL REFERENCES banks ON DELETE CASCADE,
    name                 text        NOT NULL,
    kind                 text        NOT NULL CHECK (kind IN ('current', 'card', 'savings', 'other')),
    currency             char(3)     NOT NULL DEFAULT 'EUR',
    -- "the balance was X on date D": every balance is computed from this anchor plus transactions after it
    anchor_balance_cents bigint      NOT NULL DEFAULT 0,
    anchor_date          date        NOT NULL DEFAULT current_date,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bank_id, name)
);

CREATE TABLE categories (
    id                  uuid PRIMARY KEY,
    user_id             uuid    NOT NULL REFERENCES users ON DELETE CASCADE,
    parent_id           uuid REFERENCES categories ON DELETE CASCADE,
    key                 text, -- stable key of a default category (built-in rules target it); null for user ones
    name                text    NOT NULL,
    kind                text    NOT NULL CHECK (kind IN ('expense', 'income', 'transfer')),
    icon                text    NOT NULL DEFAULT 'tag',
    color               text    NOT NULL DEFAULT '#6B7280',
    excluded_from_stats boolean NOT NULL DEFAULT false,
    position            int     NOT NULL DEFAULT 0,
    UNIQUE (user_id, key)
);

CREATE INDEX categories_user ON categories (user_id);

-- +goose Down
DROP TABLE categories;
DROP TABLE accounts;
DROP TABLE banks;
