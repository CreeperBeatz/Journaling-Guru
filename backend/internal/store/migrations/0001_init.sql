-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd

CREATE TABLE users (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             citext      NOT NULL UNIQUE,
    email_verified    boolean     NOT NULL DEFAULT false,
    display_name      text,
    timezone          text        NOT NULL DEFAULT 'UTC',
    reminder_time     time        NOT NULL DEFAULT '20:00',
    reminder_enabled  boolean     NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

CREATE INDEX users_active_idx ON users (id) WHERE deleted_at IS NULL;

CREATE TABLE magic_link_tokens (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    ip_address   inet,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Rate-limit lookups by email join + recency.
CREATE INDEX magic_link_tokens_user_created_idx
    ON magic_link_tokens (user_id, created_at DESC);

-- Rate-limit lookups by IP + recency.
CREATE INDEX magic_link_tokens_ip_created_idx
    ON magic_link_tokens (ip_address, created_at DESC)
    WHERE ip_address IS NOT NULL;

CREATE TABLE sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    ip_address   inet,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS magic_link_tokens;
DROP TABLE IF EXISTS users;
