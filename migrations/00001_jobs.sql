-- Background job queue: workers claim due jobs with FOR UPDATE SKIP LOCKED and hold them with a lease.

-- +goose Up
CREATE TABLE jobs (
    id           uuid PRIMARY KEY,
    kind         text        NOT NULL,
    user_id      uuid,
    payload      jsonb       NOT NULL DEFAULT '{}',
    run_at       timestamptz NOT NULL DEFAULT now(),
    attempts     int         NOT NULL DEFAULT 0,
    max_attempts int         NOT NULL DEFAULT 5,
    locked_until timestamptz,
    last_error   text,
    done_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Only pending jobs are ever scanned, so the index skips finished ones.
CREATE INDEX jobs_pending ON jobs (run_at) WHERE done_at IS NULL;

-- +goose Down
DROP TABLE jobs;
