-- +goose Up
-- Sub-accounts: a sub-account is a users row with parent_user_id set. It has
-- a virtual email (never deliverable), cannot deposit/withdraw, and can only
-- move funds back to its parent via the sub-account transfer API.
ALTER TABLE users ADD COLUMN parent_user_id BIGINT REFERENCES users(id);
CREATE INDEX idx_users_parent ON users (parent_user_id) WHERE parent_user_id IS NOT NULL;

CREATE TABLE sub_accounts (
    user_id       BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    parent_id     BIGINT NOT NULL REFERENCES users(id),
    username      TEXT NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    login_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (parent_id, username)
);
CREATE INDEX idx_sub_accounts_parent ON sub_accounts (parent_id);

-- API keys for sub-accounts. Only the sha256 of the secret is stored.
-- permissions: comma list of read,spot_trade,futures_trade (never withdraw).
CREATE TABLE sub_api_keys (
    id           BIGSERIAL PRIMARY KEY,
    api_key      TEXT UNIQUE NOT NULL,
    secret_hash  TEXT NOT NULL,
    sub_user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id    BIGINT NOT NULL REFERENCES users(id),
    label        TEXT NOT NULL DEFAULT '',
    permissions  TEXT NOT NULL DEFAULT 'read',
    ip_allowlist TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_sub_api_keys_sub ON sub_api_keys (sub_user_id);

-- Parent <-> sub spot transfer records (idempotent on parent + clientId).
CREATE TABLE sub_transfers (
    id          BIGSERIAL PRIMARY KEY,
    transfer_id TEXT UNIQUE NOT NULL,
    parent_id   BIGINT NOT NULL REFERENCES users(id),
    sub_user_id BIGINT NOT NULL REFERENCES users(id),
    client_id   TEXT NOT NULL,
    direction   TEXT NOT NULL CHECK (direction IN ('to_sub', 'to_parent')),
    asset       TEXT NOT NULL,
    amount      NUMERIC(38, 18) NOT NULL CHECK (amount > 0),
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'success', 'failed')),
    error_code  TEXT NOT NULL DEFAULT '',
    initiator   TEXT NOT NULL DEFAULT 'parent',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (parent_id, client_id)
);
CREATE INDEX idx_sub_transfers_parent ON sub_transfers (parent_id, id DESC);

-- Admin-configurable rules (single row).
CREATE TABLE sub_account_settings (
    id                  INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    max_per_parent      INT NOT NULL DEFAULT 20,
    require_parent_2fa  BOOLEAN NOT NULL DEFAULT TRUE,
    allow_sub_login     BOOLEAN NOT NULL DEFAULT TRUE,
    max_api_keys_per_sub INT NOT NULL DEFAULT 10,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO sub_account_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE sub_account_settings;
DROP TABLE sub_transfers;
DROP TABLE sub_api_keys;
DROP TABLE sub_accounts;
DROP INDEX IF EXISTS idx_users_parent;
ALTER TABLE users DROP COLUMN parent_user_id;
