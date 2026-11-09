-- +goose Up
-- Primary-account API keys. Only sha256(secret) is stored. permissions is
-- fixed to read: this batch cannot trade or withdraw.
CREATE TABLE user_api_keys (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key      TEXT UNIQUE NOT NULL,
    secret_hash  TEXT NOT NULL,
    label        TEXT NOT NULL DEFAULT '',
    permissions  TEXT NOT NULL DEFAULT 'read' CHECK (permissions = 'read'),
    ip_allowlist TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_user_api_keys_user ON user_api_keys (user_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE user_api_keys;
