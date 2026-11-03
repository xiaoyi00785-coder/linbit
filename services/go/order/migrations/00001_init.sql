-- +goose Up
-- Trading-pair configuration. Precision and minimums are enforced at order
-- admission. Seeded with the two default spot pairs; add rows to list more.
CREATE TABLE symbols (
    symbol             TEXT PRIMARY KEY,
    base               TEXT NOT NULL,
    quote              TEXT NOT NULL,
    price_precision    INT  NOT NULL,
    quantity_precision INT  NOT NULL,
    min_quantity       NUMERIC(36,18) NOT NULL DEFAULT 0,
    min_notional       NUMERIC(36,18) NOT NULL DEFAULT 0,
    status             TEXT NOT NULL DEFAULT 'trading'
);

INSERT INTO symbols (symbol, base, quote, price_precision, quantity_precision, min_quantity, min_notional, status) VALUES
    ('BTCUSDT', 'BTC', 'USDT', 2, 6, 0.000010, 5, 'trading'),
    ('ETHUSDT', 'ETH', 'USDT', 2, 5, 0.001,    5, 'trading');

-- Monotonic, human-readable order ids (10-digit range like the openapi example).
CREATE SEQUENCE order_id_seq START 1000000001;

CREATE TABLE orders (
    order_id        TEXT PRIMARY KEY,
    client_order_id TEXT,
    user_id         TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    side            TEXT NOT NULL,                 -- buy | sell
    type            TEXT NOT NULL,                 -- limit | market
    time_in_force   TEXT NOT NULL,                 -- GTC | IOC | FOK | PostOnly
    price           NUMERIC(36,18),                -- null for market
    quantity        NUMERIC(36,18) NOT NULL DEFAULT 0,  -- base qty (0 for market-buy-by-quote)
    quote_order_qty NUMERIC(36,18),                -- market-buy budget
    filled_quantity NUMERIC(36,18) NOT NULL DEFAULT 0,
    status          TEXT NOT NULL,                 -- new | partially_filled | filled | canceled | rejected
    frozen_asset    TEXT NOT NULL,
    frozen_amount   NUMERIC(36,18) NOT NULL DEFAULT 0,
    unfrozen        BOOLEAN NOT NULL DEFAULT false, -- guards the one-time leftover release
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_orders_user_symbol ON orders (user_id, symbol);
CREATE INDEX idx_orders_user_status ON orders (user_id, status);

-- One row per order side per matched trade (an engine trade yields two rows:
-- the maker's and the taker's). Unique (trade_id, order_id) makes event
-- re-delivery idempotent.
CREATE TABLE trades (
    id         BIGSERIAL PRIMARY KEY,
    trade_id   TEXT NOT NULL,
    order_id   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    symbol     TEXT NOT NULL,
    side       TEXT NOT NULL,
    price      NUMERIC(36,18) NOT NULL,
    quantity   NUMERIC(36,18) NOT NULL,
    fee        NUMERIC(36,18) NOT NULL DEFAULT 0,
    fee_asset  TEXT,
    role       TEXT NOT NULL,  -- maker | taker
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (trade_id, order_id)
);
CREATE INDEX idx_trades_user_symbol ON trades (user_id, symbol);
CREATE INDEX idx_trades_order ON trades (order_id);

-- +goose Down
DROP TABLE trades;
DROP TABLE orders;
DROP SEQUENCE order_id_seq;
DROP TABLE symbols;
