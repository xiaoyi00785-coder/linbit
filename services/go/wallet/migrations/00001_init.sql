-- +goose Up

-- One reusable deposit address per (user, asset, network).
CREATE TABLE deposit_addresses (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    asset      TEXT NOT NULL,
    network    TEXT NOT NULL,
    address    TEXT NOT NULL,
    memo       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, asset, network),
    UNIQUE (network, address)
);

-- Credited deposits. The (network, tx_hash, log_index) unique constraint is the
-- idempotency guarantee: one on-chain output credits a user's balance at most once.
CREATE TABLE deposits (
    id            BIGSERIAL PRIMARY KEY,
    user_id       TEXT NOT NULL,
    asset         TEXT NOT NULL,
    network       TEXT NOT NULL,
    address       TEXT NOT NULL,
    amount        NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    tx_hash       TEXT NOT NULL,
    log_index     BIGINT NOT NULL DEFAULT 0,
    confirmations BIGINT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL,              -- confirming | completed | ignored
    credited_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (network, tx_hash, log_index)
);
CREATE INDEX idx_deposits_user ON deposits (user_id);

-- Withdrawals and their state machine. status is the internal state; the API
-- maps it onto the openapi enum (pending/processing/completed/failed/canceled).
--   pending_review -> approved -> processing -> completed
--                  \-> rejected (manual) ;  canceled (user) ;  failed (broadcast)
CREATE TABLE withdrawals (
    id               BIGSERIAL PRIMARY KEY,
    user_id          TEXT NOT NULL,
    asset            TEXT NOT NULL,
    network          TEXT NOT NULL,
    address          TEXT NOT NULL,
    memo             TEXT NOT NULL DEFAULT '',
    amount           NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    gas_fee          NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (gas_fee >= 0),
    gas_fee_asset    TEXT NOT NULL,
    gas_deduct_mode  TEXT NOT NULL,           -- extra | amount
    frozen_amount    NUMERIC(36,18) NOT NULL CHECK (frozen_amount > 0),
    risk_score       INT NOT NULL DEFAULT 0,
    status           TEXT NOT NULL,
    tx_hash          TEXT,
    broadcast_height BIGINT,
    reject_reason    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_withdrawals_user ON withdrawals (user_id);
CREATE INDEX idx_withdrawals_status ON withdrawals (status);

-- Simulated chain state (per network block height). No real node is contacted.
CREATE TABLE chain_state (
    network TEXT PRIMARY KEY,
    height  BIGINT NOT NULL DEFAULT 0
);

-- Simulated on-chain deposit transactions awaiting enough confirmations. The
-- scanner reads these, matches an allocated deposit address, and credits the
-- user once confirmations are sufficient. Injected via the dev-only endpoint.
CREATE TABLE chain_txs (
    id           BIGSERIAL PRIMARY KEY,
    network      TEXT NOT NULL,
    address      TEXT NOT NULL,
    asset        TEXT NOT NULL,
    amount       NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    tx_hash      TEXT NOT NULL,
    log_index    BIGINT NOT NULL DEFAULT 0,
    block_height BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (network, tx_hash, log_index)
);

-- +goose Down
DROP TABLE chain_txs;
DROP TABLE chain_state;
DROP TABLE withdrawals;
DROP TABLE deposits;
DROP TABLE deposit_addresses;
