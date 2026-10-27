-- One categorization model per user (encrypted gob of the token counts) and its evaluation.

-- +goose Up
CREATE TABLE models (
    user_id        uuid PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    trained_at     timestamptz NOT NULL DEFAULT now(),
    samples        int         NOT NULL,
    tested         int         NOT NULL,
    accuracy       real        NOT NULL,
    user_tested    int         NOT NULL,
    model_on_user  real        NOT NULL,
    rules_on_user  real        NOT NULL,
    active         boolean     NOT NULL,
    state_enc      bytea       NOT NULL
);

-- +goose Down
DROP TABLE models;
