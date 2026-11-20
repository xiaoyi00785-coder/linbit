-- +goose Up
-- Options trading: European-style cash-settled crypto options (Deribit-style).
-- An instrument is one (underlying, expiry, strike, type:call|put). Orders hit
-- a per-instrument price-time order book (opt_orders is the book of record).
-- Mark price + Greeks are refreshed by a ticker from Black-Scholes using the
-- spot index from market-data; expiry settlement cash-settles ITM positions
-- against system:options through the asset ledger.

CREATE TABLE opt_config (
    id              INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    risk_free_rate  NUMERIC(10,6) NOT NULL DEFAULT 0.05,
    fee_rate        NUMERIC(10,6) NOT NULL DEFAULT 0.0003,    -- 3 bps per side, Deribit-style
    min_premium     NUMERIC(36,8) NOT NULL DEFAULT 0.0001,
    max_order_qty   NUMERIC(36,8) NOT NULL DEFAULT 1000,
    settle_grace_s  INT NOT NULL DEFAULT 60,                  -- expiry settlement grace
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO opt_config DEFAULT VALUES;

-- Instruments. Name convention: {BASE}-{DDMMMYY}-{STRIKE}-{C|P}
-- e.g. BTC-30DEC26-50000-C.
CREATE TABLE opt_instruments (
    id               BIGSERIAL PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    underlying       TEXT NOT NULL,                 -- BTC, ETH, SOL
    quote            TEXT NOT NULL DEFAULT 'USDT',
    option_type      TEXT NOT NULL CHECK (option_type IN ('call','put')),
    strike           NUMERIC(36,8) NOT NULL CHECK (strike > 0),
    expiry           TIMESTAMPTZ NOT NULL,          -- UTC 08:00 is the Deribit standard
    contract_size    NUMERIC(36,8) NOT NULL DEFAULT 1,   -- 1 underlying unit per contract
    tick_size        NUMERIC(36,8) NOT NULL DEFAULT 0.0001,
    min_qty          NUMERIC(36,8) NOT NULL DEFAULT 0.1,
    status           TEXT NOT NULL CHECK (status IN ('active','expired','halted')) DEFAULT 'active',
    settlement_price NUMERIC(36,8),                 -- set at expiry from the index
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_opt_instruments_chain ON opt_instruments (underlying, expiry, status);
CREATE INDEX idx_opt_instruments_status ON opt_instruments (status, expiry);

-- Orders / book of record.
CREATE TABLE opt_orders (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    instrument_id   BIGINT NOT NULL REFERENCES opt_instruments(id),
    client_id       TEXT,
    side            TEXT NOT NULL CHECK (side IN ('buy','sell')),
    order_type      TEXT NOT NULL CHECK (order_type IN ('limit','market')),
    price           NUMERIC(36,8),                   -- NULL for market
    qty             NUMERIC(36,8) NOT NULL CHECK (qty > 0),
    filled_qty      NUMERIC(36,8) NOT NULL DEFAULT 0,
    avg_fill_price  NUMERIC(36,8),
    status          TEXT NOT NULL CHECK (status IN ('open','partial','filled','cancelled','rejected')) DEFAULT 'open',
    reject_reason   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_opt_orders_book  ON opt_orders (instrument_id, status, price, id)
    WHERE status IN ('open','partial');
CREATE INDEX idx_opt_orders_user  ON opt_orders (user_id, created_at DESC);
CREATE UNIQUE INDEX uq_opt_orders_client ON opt_orders (user_id, client_id) WHERE client_id IS NOT NULL;

-- Trades (fill log).
CREATE TABLE opt_trades (
    id              BIGSERIAL PRIMARY KEY,
    instrument_id   BIGINT NOT NULL REFERENCES opt_instruments(id),
    maker_order_id  BIGINT NOT NULL REFERENCES opt_orders(id),
    taker_order_id  BIGINT NOT NULL REFERENCES opt_orders(id),
    maker_user_id   TEXT NOT NULL,
    taker_user_id   TEXT NOT NULL,
    taker_side      TEXT NOT NULL CHECK (taker_side IN ('buy','sell')),
    price           NUMERIC(36,8) NOT NULL,
    qty             NUMERIC(36,8) NOT NULL,
    maker_fee       NUMERIC(36,8) NOT NULL DEFAULT 0,
    taker_fee       NUMERIC(36,8) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_opt_trades_instrument ON opt_trades (instrument_id, id DESC);
CREATE INDEX idx_opt_trades_taker ON opt_trades (taker_user_id, id DESC);
CREATE INDEX idx_opt_trades_maker ON opt_trades (maker_user_id, id DESC);

-- Net positions per (user, instrument). Signed qty: +long, -short.
CREATE TABLE opt_positions (
    user_id         TEXT NOT NULL,
    instrument_id   BIGINT NOT NULL REFERENCES opt_instruments(id),
    qty             NUMERIC(36,8) NOT NULL DEFAULT 0,
    avg_price       NUMERIC(36,8) NOT NULL DEFAULT 0,  -- weighted entry premium
    realized_pnl    NUMERIC(36,8) NOT NULL DEFAULT 0,
    margin          NUMERIC(36,8) NOT NULL DEFAULT 0,  -- held for short exposure
    settled         BOOLEAN NOT NULL DEFAULT false,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, instrument_id)
);
CREATE INDEX idx_opt_positions_user ON opt_positions (user_id) WHERE qty <> 0;
CREATE INDEX idx_opt_positions_instr ON opt_positions (instrument_id) WHERE qty <> 0;

-- Mark price + Greeks snapshot per instrument. One live row per instrument;
-- callers UPSERT every tick. opt_marks_history keeps a down-sampled series
-- for charts (populated by the worker).
CREATE TABLE opt_marks (
    instrument_id   BIGINT PRIMARY KEY REFERENCES opt_instruments(id),
    mark_price      NUMERIC(36,8) NOT NULL,
    underlying      NUMERIC(36,8) NOT NULL,
    iv              NUMERIC(10,6) NOT NULL,   -- annualised vol used
    delta           NUMERIC(10,6) NOT NULL,
    gamma           NUMERIC(14,10) NOT NULL,
    theta           NUMERIC(20,10) NOT NULL,  -- per day
    vega            NUMERIC(20,10) NOT NULL,  -- per 1% vol
    ts              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE opt_marks_history (
    id              BIGSERIAL PRIMARY KEY,
    instrument_id   BIGINT NOT NULL REFERENCES opt_instruments(id),
    mark_price      NUMERIC(36,8) NOT NULL,
    underlying      NUMERIC(36,8) NOT NULL,
    iv              NUMERIC(10,6) NOT NULL,
    ts              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_opt_marks_history ON opt_marks_history (instrument_id, ts DESC);

-- Settlement ledger (one row per settled position; drives idempotent payout).
CREATE TABLE opt_settlements (
    id              BIGSERIAL PRIMARY KEY,
    instrument_id   BIGINT NOT NULL REFERENCES opt_instruments(id),
    user_id         TEXT NOT NULL,
    qty             NUMERIC(36,8) NOT NULL,            -- signed
    settle_price    NUMERIC(36,8) NOT NULL,            -- underlying at expiry
    payout          NUMERIC(36,8) NOT NULL,            -- paid to the user (positive) or collected (negative)
    status          TEXT NOT NULL CHECK (status IN ('pending','paid','failed')) DEFAULT 'pending',
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at         TIMESTAMPTZ,
    UNIQUE (instrument_id, user_id)
);
CREATE INDEX idx_opt_settlements_status ON opt_settlements (status, created_at);

-- Seed a few instruments so the service boots with a live chain. Admin can
-- extend this at runtime (POST /internal/dev/instruments, to be added).
INSERT INTO opt_instruments (name, underlying, option_type, strike, expiry) VALUES
  ('BTC-26DEC26-80000-C', 'BTC', 'call',  80000, '2026-12-26 08:00:00+00'),
  ('BTC-26DEC26-80000-P', 'BTC', 'put',   80000, '2026-12-26 08:00:00+00'),
  ('BTC-26DEC26-100000-C','BTC', 'call', 100000, '2026-12-26 08:00:00+00'),
  ('BTC-26DEC26-100000-P','BTC', 'put',  100000, '2026-12-26 08:00:00+00'),
  ('BTC-26DEC26-120000-C','BTC', 'call', 120000, '2026-12-26 08:00:00+00'),
  ('BTC-26DEC26-120000-P','BTC', 'put',  120000, '2026-12-26 08:00:00+00'),
  ('ETH-26DEC26-3000-C',  'ETH', 'call',   3000, '2026-12-26 08:00:00+00'),
  ('ETH-26DEC26-3000-P',  'ETH', 'put',    3000, '2026-12-26 08:00:00+00'),
  ('ETH-26DEC26-4000-C',  'ETH', 'call',   4000, '2026-12-26 08:00:00+00'),
  ('ETH-26DEC26-4000-P',  'ETH', 'put',    4000, '2026-12-26 08:00:00+00');

-- +goose Down
DROP TABLE IF EXISTS opt_settlements;
DROP TABLE IF EXISTS opt_marks_history;
DROP TABLE IF EXISTS opt_marks;
DROP TABLE IF EXISTS opt_positions;
DROP TABLE IF EXISTS opt_trades;
DROP TABLE IF EXISTS opt_orders;
DROP TABLE IF EXISTS opt_instruments;
DROP TABLE IF EXISTS opt_config;
