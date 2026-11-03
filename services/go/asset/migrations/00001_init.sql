-- +goose Up
-- One asset account per user.
CREATE TABLE asset_accounts (
    user_id    TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Materialised per-user, per-asset balance. The CHECK constraints are the
-- last line of defence guaranteeing balances can never go negative.
CREATE TABLE balances (
    user_id TEXT NOT NULL,
    asset   TEXT NOT NULL,
    free    NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (free >= 0),
    locked  NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (locked >= 0),
    PRIMARY KEY (user_id, asset)
);

-- Double-entry journal: every financial event is one transaction whose entries
-- must balance (sum of debits = sum of credits per asset).
CREATE TABLE ledger_transactions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    tx_type    TEXT NOT NULL,
    ref        TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
    id        BIGSERIAL PRIMARY KEY,
    tx_id     BIGINT NOT NULL REFERENCES ledger_transactions (id) ON DELETE CASCADE,
    account   TEXT NOT NULL,
    asset     TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('debit', 'credit')),
    amount    NUMERIC(36,18) NOT NULL CHECK (amount > 0)
);
CREATE INDEX idx_ledger_entries_tx ON ledger_entries (tx_id);
CREATE INDEX idx_ledger_tx_user ON ledger_transactions (user_id);

-- +goose Down
DROP TABLE ledger_entries;
DROP TABLE ledger_transactions;
DROP TABLE balances;
DROP TABLE asset_accounts;
