-- +goose Up
-- Funding account: a second per-user wallet next to spot (balances). On-chain
-- deposits land here by default and withdrawals are paid from here; users move
-- funds funding <-> spot <-> futures via /assets/transfer. Same shape and
-- non-negative invariant as balances.
CREATE TABLE funding_balances (
    user_id    TEXT NOT NULL,
    asset      TEXT NOT NULL,
    free       NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (free >= 0),
    locked     NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (locked >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, asset)
);

-- Runtime settings (admin-editable). deposit_account / withdraw_account choose
-- which wallet on-chain deposits credit and withdrawals freeze from.
CREATE TABLE asset_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO asset_settings (key, value) VALUES
    ('deposit_account', 'funding'),
    ('withdraw_account', 'funding');

-- Which wallet a freeze ref was taken from, so unfreeze / withdraw-complete on
-- the same ref hit the same wallet even if the setting changes in between.
-- Refs with no row are spot (all pre-existing freezes).
CREATE TABLE freeze_accounts (
    ref        TEXT PRIMARY KEY,
    account    TEXT NOT NULL CHECK (account IN ('spot','funding')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE freeze_accounts;
DROP TABLE asset_settings;
DROP TABLE funding_balances;
