-- +goose Up
-- Public uid starts at 80000001 (matches docs/api/openapi.yaml examples).
CREATE SEQUENCE IF NOT EXISTS uid_seq START 80000001;

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    uid           TEXT UNIQUE NOT NULL DEFAULT nextval('uid_seq')::text,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    nickname      TEXT,
    kyc_level     INT NOT NULL DEFAULT 0,
    totp_secret   TEXT NOT NULL DEFAULT '',
    totp_enabled  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    revoked    BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens (token_hash);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE users;
DROP SEQUENCE uid_seq;
