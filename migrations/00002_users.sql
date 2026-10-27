-- Users and their refresh tokens (rotated on every use; a reused token revokes its whole family).

-- +goose Up
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    email         text        NOT NULL UNIQUE, -- stored lowercased
    password_hash text        NOT NULL,        -- argon2id PHC string
    dek_wrapped   bytea       NOT NULL,        -- per-user data key, wrapped by the master key
    locale        text        NOT NULL DEFAULT 'fr',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users ON DELETE CASCADE,
    family_id  uuid        NOT NULL,
    token_hash bytea       NOT NULL UNIQUE, -- sha256 of the token; the token itself is never stored
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    revoked_at timestamptz,
    user_agent text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_family ON refresh_tokens (family_id);

ALTER TABLE jobs ADD FOREIGN KEY (user_id) REFERENCES users ON DELETE CASCADE;

-- +goose Down
ALTER TABLE jobs DROP CONSTRAINT jobs_user_id_fkey;
DROP TABLE refresh_tokens;
DROP TABLE users;
