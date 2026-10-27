-- Import profiles (saved column mappings), imports with their staged rows, and transactions.

-- +goose Up
CREATE TABLE import_profiles (
    id              uuid PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    fingerprint     text        NOT NULL, -- sha256 of the normalized header row: recognizes the format next time
    name            text        NOT NULL,
    mapping         jsonb       NOT NULL,
    last_account_id uuid REFERENCES accounts ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, fingerprint)
);

CREATE TABLE imports (
    id             uuid PRIMARY KEY,
    user_id        uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    account_id     uuid REFERENCES accounts ON DELETE CASCADE,
    profile_id     uuid REFERENCES import_profiles ON DELETE SET NULL,
    status         text        NOT NULL DEFAULT 'uploaded'
        CHECK (status IN ('uploaded', 'mapped', 'committed', 'cancelled')),
    file_name_enc  bytea       NOT NULL,
    file_kind      text        NOT NULL,
    fingerprint    text        NOT NULL,
    grid_enc       bytea, -- raw cells, encrypted; dropped on commit or cancel
    rows_total     int         NOT NULL DEFAULT 0,
    rows_new       int         NOT NULL DEFAULT 0,
    rows_duplicate int         NOT NULL DEFAULT 0,
    rows_review    int         NOT NULL DEFAULT 0,
    rows_invalid   int         NOT NULL DEFAULT 0,
    period_start   date,
    period_end     date,
    created_at     timestamptz NOT NULL DEFAULT now(),
    committed_at   timestamptz
);

CREATE INDEX imports_user ON imports (user_id, created_at DESC);

CREATE TABLE transactions (
    id              uuid PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    account_id      uuid        NOT NULL REFERENCES accounts ON DELETE CASCADE,
    import_id       uuid REFERENCES imports ON DELETE SET NULL,
    booked_on       date        NOT NULL,
    amount_cents    bigint      NOT NULL, -- negative = money out
    label_enc       bytea       NOT NULL,
    merchant_enc    bytea       NOT NULL,
    merchant_key    bytea       NOT NULL, -- blind index of the merchant
    dedup_key       bytea       NOT NULL, -- blind index of date|amount|normalized label
    occurrence      int         NOT NULL, -- n-th identical row in its file
    category_id     uuid REFERENCES categories ON DELETE SET NULL,
    category_source text CHECK (category_source IN ('rule', 'builtin', 'model', 'user', 'transfer')),
    confidence      real,
    transfer_id     uuid,
    recurring_id    uuid,
    note_enc        bytea,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX transactions_user_date ON transactions (user_id, booked_on DESC, id DESC);
CREATE INDEX transactions_account_dedup ON transactions (account_id, dedup_key);
CREATE INDEX transactions_account_date ON transactions (account_id, booked_on);
CREATE INDEX transactions_merchant ON transactions (user_id, merchant_key);
CREATE INDEX transactions_import ON transactions (import_id);

-- Rows of a mapped import waiting for commit.
CREATE TABLE import_rows (
    import_id    uuid NOT NULL REFERENCES imports ON DELETE CASCADE,
    row_index    int  NOT NULL, -- row number in the file (0-based)
    booked_on    date,
    amount_cents bigint,
    label_enc    bytea,
    dedup_key    bytea,
    occurrence   int,
    status       text NOT NULL CHECK (status IN ('new', 'duplicate', 'review', 'invalid')),
    match_txn_id uuid REFERENCES transactions ON DELETE SET NULL,
    error        text,
    PRIMARY KEY (import_id, row_index)
);

-- +goose Down
DROP TABLE import_rows;
DROP TABLE transactions;
DROP TABLE imports;
DROP TABLE import_profiles;
