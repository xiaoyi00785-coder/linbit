-- +goose Up
-- Admin overrides layered over the built-in / WALLET_NETWORKS_JSON registry.
-- NULL = keep the registry value.
CREATE TABLE wallet_network_settings (
    asset            TEXT NOT NULL,
    network          TEXT NOT NULL,
    confirmations    INT CHECK (confirmations IS NULL OR confirmations >= 0),
    min_deposit      NUMERIC(36,18) CHECK (min_deposit IS NULL OR min_deposit >= 0),
    min_withdraw     NUMERIC(36,18) CHECK (min_withdraw IS NULL OR min_withdraw >= 0),
    max_withdraw     NUMERIC(36,18) CHECK (max_withdraw IS NULL OR max_withdraw >= 0),
    withdraw_fee     NUMERIC(36,18) CHECK (withdraw_fee IS NULL OR withdraw_fee >= 0),
    memo_required    BOOLEAN,
    deposit_enabled  BOOLEAN,
    withdraw_enabled BOOLEAN,
    updated_by       TEXT NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (asset, network)
);

CREATE TABLE wallet_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO wallet_settings (key, value) VALUES
    ('internal_transfer_enabled', 'true'),
    ('internal_transfer_daily_limit_usdt', '50000')
ON CONFLICT DO NOTHING;

-- Off-chain user-to-user transfers (funding -> funding). (user_id, client_id)
-- is the idempotency key; the asset ledger leg uses ref itx:<id>.
CREATE TABLE internal_transfers (
    id           BIGSERIAL PRIMARY KEY,
    user_id      TEXT NOT NULL,
    to_user_id   TEXT NOT NULL,
    client_id    TEXT NOT NULL,
    asset        TEXT NOT NULL,
    amount       NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    value_usdt   NUMERIC(36,18) NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending | completed | failed
    fail_reason  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    UNIQUE (user_id, client_id),
    CHECK (user_id <> to_user_id)
);
CREATE INDEX idx_internal_transfers_user_day ON internal_transfers (user_id, created_at);
CREATE INDEX idx_internal_transfers_to ON internal_transfers (to_user_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS internal_transfers;
DROP TABLE IF EXISTS wallet_settings;
DROP TABLE IF EXISTS wallet_network_settings;
