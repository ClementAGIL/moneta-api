-- Recurring payments and income (subscriptions, bills, salary), detected from the transactions.

-- +goose Up
CREATE TABLE recurring (
    id                    uuid PRIMARY KEY,
    user_id               uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    merchant_key          bytea       NOT NULL,
    display_enc           bytea       NOT NULL,
    sign                  text        NOT NULL CHECK (sign IN ('in', 'out')),
    cadence               text        NOT NULL CHECK (cadence IN ('weekly', 'monthly', 'quarterly', 'yearly')),
    amount_cents          bigint      NOT NULL, -- latest amount, signed
    previous_amount_cents bigint,               -- amount before the latest price change
    price_changed_on      date,
    avg_cents             bigint      NOT NULL,
    occurrences           int         NOT NULL,
    last_date             date        NOT NULL,
    next_date             date        NOT NULL,
    category_id           uuid REFERENCES categories ON DELETE SET NULL,
    ignored               boolean     NOT NULL DEFAULT false, -- set by the user, survives re-detection
    subscription          boolean     NOT NULL,               -- detected guess
    subscription_override boolean,                            -- user's answer, wins over the guess
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, merchant_key, sign)
);

ALTER TABLE transactions ADD FOREIGN KEY (recurring_id) REFERENCES recurring ON DELETE SET NULL;
CREATE INDEX transactions_transfer ON transactions (transfer_id) WHERE transfer_id IS NOT NULL;

-- +goose Down
DROP INDEX transactions_transfer;
ALTER TABLE transactions DROP CONSTRAINT transactions_recurring_id_fkey;
DROP TABLE recurring;
