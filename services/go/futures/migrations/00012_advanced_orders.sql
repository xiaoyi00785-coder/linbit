-- +goose Up
-- Advanced futures orders: hedge mode (position_side), position history, order
-- priority sequence (amend), POST_ONLY, algo orders (partial TP/SL + plan/trigger).

-- Hedge mode: one row per (user, symbol, side). BOTH = one-way mode.
ALTER TABLE fut_positions ADD COLUMN position_side TEXT NOT NULL DEFAULT 'BOTH'
    CHECK (position_side IN ('BOTH','LONG','SHORT'));
ALTER TABLE fut_positions DROP CONSTRAINT fut_positions_pkey;
ALTER TABLE fut_positions ADD PRIMARY KEY (user_id, symbol, position_side);

-- Lifecycle accumulators for position history (reset when a position opens).
ALTER TABLE fut_positions
    ADD COLUMN opened_at   TIMESTAMPTZ,
    ADD COLUMN open_qty    NUMERIC(36,18) NOT NULL DEFAULT 0, -- total qty opened
    ADD COLUMN open_quote  NUMERIC(36,18) NOT NULL DEFAULT 0, -- sum(opened qty * price)
    ADD COLUMN close_qty   NUMERIC(36,18) NOT NULL DEFAULT 0,
    ADD COLUMN close_quote NUMERIC(36,18) NOT NULL DEFAULT 0,
    ADD COLUMN max_qty     NUMERIC(36,18) NOT NULL DEFAULT 0,
    ADD COLUMN acc_fee     NUMERIC(36,18) NOT NULL DEFAULT 0,
    ADD COLUMN acc_funding NUMERIC(36,18) NOT NULL DEFAULT 0; -- >0 received, <0 paid
UPDATE fut_positions SET opened_at = updated_at, open_qty = abs(qty), open_quote = abs(qty)*entry_price, max_qty = abs(qty) WHERE qty <> 0;

CREATE TABLE fut_position_history (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    position_side   TEXT NOT NULL,
    direction       TEXT NOT NULL CHECK (direction IN ('long','short')),
    margin_mode     TEXT NOT NULL,
    leverage        INT NOT NULL,
    entry_price     NUMERIC(36,18) NOT NULL, -- average open price
    close_price     NUMERIC(36,18) NOT NULL, -- average close price
    max_qty         NUMERIC(36,18) NOT NULL,
    closed_qty      NUMERIC(36,18) NOT NULL,
    realized_pnl    NUMERIC(36,18) NOT NULL, -- price PnL only
    fees            NUMERIC(36,18) NOT NULL, -- trading fees paid (positive)
    funding         NUMERIC(36,18) NOT NULL, -- net funding (>0 received)
    net_pnl         NUMERIC(36,18) NOT NULL, -- realized - fees + funding
    status          TEXT NOT NULL,           -- closed | liquidated | adl
    opened_at       TIMESTAMPTZ NOT NULL,
    closed_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_position_history_user ON fut_position_history (user_id, id DESC);

-- Account-wide position mode (Binance style).
CREATE TABLE fut_position_mode (
    user_id    TEXT PRIMARY KEY,
    mode       TEXT NOT NULL CHECK (mode IN ('ONE_WAY','HEDGE')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Orders: hedge side, priority sequence (book order is price, seq), POST_ONLY,
-- amend counter, algo origin.
ALTER TABLE fut_orders ADD COLUMN position_side TEXT NOT NULL DEFAULT 'BOTH';
CREATE SEQUENCE fut_orders_seq;
ALTER TABLE fut_orders ADD COLUMN seq BIGINT;
UPDATE fut_orders SET seq = id;
SELECT setval('fut_orders_seq', GREATEST((SELECT COALESCE(max(id),0) FROM fut_orders), 1));
ALTER TABLE fut_orders ALTER COLUMN seq SET DEFAULT nextval('fut_orders_seq');
ALTER TABLE fut_orders ALTER COLUMN seq SET NOT NULL;
ALTER TABLE fut_orders ADD COLUMN amend_count INT NOT NULL DEFAULT 0;
ALTER TABLE fut_orders ADD COLUMN algo_id BIGINT;
DROP INDEX fut_orders_book;
CREATE INDEX fut_orders_book ON fut_orders (symbol, side, price, seq) WHERE status IN ('new','partially_filled') AND type = 'limit';

-- Algo orders: TP/SL (partial or whole position) and plan (trigger) orders.
-- Nothing is reserved until the trigger fires and the real order is placed.
CREATE TABLE fut_algo_orders (
    id             BIGSERIAL PRIMARY KEY,
    user_id        TEXT NOT NULL,
    symbol         TEXT NOT NULL REFERENCES fut_contracts(symbol),
    kind           TEXT NOT NULL CHECK (kind IN ('tp','sl','plan')),
    side           TEXT NOT NULL CHECK (side IN ('buy','sell')),
    position_side  TEXT NOT NULL DEFAULT 'BOTH',
    order_type     TEXT NOT NULL CHECK (order_type IN ('limit','market')),
    price          NUMERIC(36,18),           -- limit price after trigger
    qty            NUMERIC(36,18),           -- NULL = whole position (tp/sl only)
    trigger_price  NUMERIC(36,18) NOT NULL,
    trigger_type   TEXT NOT NULL CHECK (trigger_type IN ('LAST','MARK')),
    trigger_dir    TEXT NOT NULL CHECK (trigger_dir IN ('up','down')), -- fire when price >= (up) / <= (down)
    reduce_only    BOOLEAN NOT NULL DEFAULT false,
    tif            TEXT NOT NULL DEFAULT 'GTC',
    status         TEXT NOT NULL DEFAULT 'active', -- active | triggered | canceled | failed
    order_id       BIGINT,
    fail_reason    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    triggered_at   TIMESTAMPTZ
);
CREATE INDEX fut_algo_active ON fut_algo_orders (symbol) WHERE status = 'active';
CREATE INDEX fut_algo_user ON fut_algo_orders (user_id, id DESC);

-- Admin-tunable rules (single row).
CREATE TABLE fut_order_config (
    id                        INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    max_algo_per_symbol       INT NOT NULL DEFAULT 10,     -- active TP/SL + plan per user per symbol
    max_amends_per_order      INT NOT NULL DEFAULT 10000,
    tpsl_default_trigger      TEXT NOT NULL DEFAULT 'MARK',
    plan_default_trigger      TEXT NOT NULL DEFAULT 'LAST',
    plan_max_deviation        NUMERIC(36,18) NOT NULL DEFAULT 0.5, -- trigger within ±50% of reference price
    hedge_mode_enabled        BOOLEAN NOT NULL DEFAULT true,
    post_only_enabled         BOOLEAN NOT NULL DEFAULT true,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO fut_order_config (id) VALUES (1);

-- +goose Down
DROP TABLE fut_order_config, fut_algo_orders, fut_position_mode, fut_position_history;
DROP INDEX fut_orders_book;
CREATE INDEX fut_orders_book ON fut_orders (symbol, side, price, id) WHERE status IN ('new','partially_filled') AND type = 'limit';
ALTER TABLE fut_orders DROP COLUMN algo_id, DROP COLUMN amend_count, DROP COLUMN seq, DROP COLUMN position_side;
DROP SEQUENCE fut_orders_seq;
DELETE FROM fut_positions WHERE position_side <> 'BOTH';
ALTER TABLE fut_positions DROP CONSTRAINT fut_positions_pkey;
ALTER TABLE fut_positions ADD PRIMARY KEY (user_id, symbol);
ALTER TABLE fut_positions DROP COLUMN acc_funding, DROP COLUMN acc_fee, DROP COLUMN max_qty, DROP COLUMN close_quote,
    DROP COLUMN close_qty, DROP COLUMN open_quote, DROP COLUMN open_qty, DROP COLUMN opened_at, DROP COLUMN position_side;
