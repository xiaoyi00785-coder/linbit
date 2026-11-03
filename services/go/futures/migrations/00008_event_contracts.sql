-- +goose Up
-- Event contracts (binary up/down predictions). Stakes move through the asset
-- ledger against system:event; this schema is the order book of record and
-- the settlement state machine:
--   pending  -> stake debit requested (retried until the asset service answers)
--   open     -> stake held, waiting for expiry
--   settling -> outcome fixed, payout credit requested (retried, idempotent ref)
--   settled  -> final (won / lost / tie / refunded)
--   rejected -> stake debit refused (insufficient balance); nothing moved
CREATE TABLE ev_config (
    id              INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    periods         TEXT[] NOT NULL,
    payout_ratio    NUMERIC(10,4) NOT NULL,
    min_stake       NUMERIC(36,8) NOT NULL,
    max_stake       NUMERIC(36,8) NOT NULL,
    daily_limit     NUMERIC(36,8) NOT NULL,
    exposure_limit  NUMERIC(36,8) NOT NULL,
    settle_grace_s  INT NOT NULL,
    updated_by      TEXT NOT NULL DEFAULT 'system',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ev_symbols (
    symbol          TEXT PRIMARY KEY,
    paused          BOOLEAN NOT NULL DEFAULT false,
    exposure_limit  NUMERIC(36,8),          -- NULL = ev_config.exposure_limit
    sort            INT NOT NULL DEFAULT 0,
    updated_by      TEXT NOT NULL DEFAULT 'system',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO ev_symbols (symbol, sort) VALUES ('BTCUSDT',1),('ETHUSDT',2),('SOLUSDT',3),('BNBUSDT',4),('TRXUSDT',5);

CREATE TABLE ev_orders (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    client_id       TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    period          TEXT NOT NULL,
    direction       TEXT NOT NULL CHECK (direction IN ('up','down')),
    stake           NUMERIC(36,8) NOT NULL,
    payout_ratio    NUMERIC(10,4) NOT NULL,
    open_price      NUMERIC(36,12) NOT NULL,
    open_time       TIMESTAMPTZ NOT NULL,
    expiry_time     TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending','open','settling','settled','rejected')),
    result          TEXT CHECK (result IN ('won','lost','tie','refunded')),
    settle_price    NUMERIC(36,12),
    payout          NUMERIC(36,8) NOT NULL DEFAULT 0,
    settled_at      TIMESTAMPTZ,
    reject_reason   TEXT,
    pushed          BOOLEAN NOT NULL DEFAULT false,   -- settlement/rejection sent over WS
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, client_id)
);
CREATE INDEX ev_orders_user ON ev_orders (user_id, id DESC);
CREATE INDEX ev_orders_work ON ev_orders (status, expiry_time) WHERE status IN ('pending','open','settling');
CREATE INDEX ev_orders_unpushed ON ev_orders (id) WHERE NOT pushed AND status IN ('settled','rejected');
CREATE INDEX ev_orders_book ON ev_orders (symbol, period, status);

-- Index samples (1/s per symbol) used to fix the expiry price after restarts.
CREATE TABLE ev_index_samples (
    symbol  TEXT NOT NULL,
    ts      TIMESTAMPTZ NOT NULL,
    price   NUMERIC(36,12) NOT NULL,
    PRIMARY KEY (symbol, ts)
);

-- +goose Down
DROP TABLE ev_index_samples;
DROP TABLE ev_orders;
DROP TABLE ev_symbols;
DROP TABLE ev_config;
