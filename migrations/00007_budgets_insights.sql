-- Monthly budgets per category, and generated insights (rule-based advice) per month.

-- +goose Up
CREATE TABLE budgets (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    category_id  uuid        NOT NULL REFERENCES categories ON DELETE CASCADE,
    amount_cents bigint      NOT NULL CHECK (amount_cents > 0),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, category_id)
);

-- params hold ids and amounts only (never labels or merchant names): clients resolve names through the API.
CREATE TABLE insights (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    kind         text        NOT NULL,
    period       date        NOT NULL, -- first day of the month it is about
    ref          text        NOT NULL DEFAULT '', -- what it is about (category, series…), so regenerating updates it
    severity     text        NOT NULL CHECK (severity IN ('info', 'good', 'warn', 'alert')),
    params       jsonb       NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now(),
    dismissed_at timestamptz,
    UNIQUE (user_id, kind, period, ref)
);

-- First payment of a recurring series: "new subscription" means a recent first payment, not a recent detection.
ALTER TABLE recurring ADD COLUMN first_date date;

-- +goose Down
ALTER TABLE recurring DROP COLUMN first_date;
DROP TABLE insights;
DROP TABLE budgets;
