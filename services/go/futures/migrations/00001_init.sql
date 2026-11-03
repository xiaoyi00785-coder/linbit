-- +goose Up
-- USDT-margined perpetual futures. All money columns are USDT, NUMERIC(36,18).

CREATE TABLE fut_contracts (
    symbol        TEXT PRIMARY KEY,           -- e.g. BTCUSDT
    base          TEXT NOT NULL,
    quote         TEXT NOT NULL DEFAULT 'USDT',
    max_leverage  INT NOT NULL,
    mmr           NUMERIC(36,18) NOT NULL,    -- maintenance margin rate
    tick_size     NUMERIC(36,18) NOT NULL,
    min_qty       NUMERIC(36,18) NOT NULL,    -- also the quantity step
    max_qty       NUMERIC(36,18) NOT NULL,    -- per order
    maker_fee     NUMERIC(36,18) NOT NULL,
    taker_fee     NUMERIC(36,18) NOT NULL,
    status        TEXT NOT NULL DEFAULT 'trading', -- trading | halted
    sort          INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Contract list follows the futures UI (feat/ui-p0-flows). BTC/ETH 200x, MMR 0.40%,
-- 0.05% maker/taker charged on open and on close by position value.
INSERT INTO fut_contracts (symbol, base, max_leverage, mmr, tick_size, min_qty, max_qty, maker_fee, taker_fee, sort) VALUES
 ('BTCUSDT',  'BTC',  200, 0.004, 0.1,     0.0001, 100,       0.0005, 0.0005, 1),
 ('ETHUSDT',  'ETH',  200, 0.004, 0.01,    0.001,  2000,      0.0005, 0.0005, 2),
 ('BNBUSDT',  'BNB',  100, 0.004, 0.01,    0.01,   5000,      0.0005, 0.0005, 3),
 ('SOLUSDT',  'SOL',  100, 0.004, 0.01,    0.01,   50000,     0.0005, 0.0005, 4),
 ('XRPUSDT',  'XRP',  100, 0.004, 0.0001,  1,      2000000,   0.0005, 0.0005, 5),
 ('DOGEUSDT', 'DOGE', 100, 0.004, 0.00001, 10,    20000000,  0.0005, 0.0005, 6),
 ('ARBUSDT',  'ARB',  75, 0.004,  0.0001,  1,      2000000,   0.0005, 0.0005, 7),
 ('TONUSDT',  'TON',  75, 0.004,  0.001,   0.1,    500000,    0.0005, 0.0005, 8),
 ('LINKUSDT', 'LINK', 75, 0.004, 0.001,  0.1,    200000,    0.0005, 0.0005, 9),
 ('AVAXUSDT', 'AVAX', 75, 0.004, 0.001,  0.1,    200000,    0.0005, 0.0005, 10);

-- One futures wallet per user. balance = wallet balance (realized); frozen = part
-- of balance reserved by open orders. Isolated margin lives on the position.
-- System accounts: system:insurance (insurance fund), system:fee (fee income).
CREATE TABLE fut_accounts (
    user_id    TEXT PRIMARY KEY,
    balance    NUMERIC(36,18) NOT NULL DEFAULT 0,
    frozen     NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (frozen >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO fut_accounts (user_id) VALUES ('system:insurance'), ('system:fee');

-- Per user+symbol settings (one-way / merged position mode).
CREATE TABLE fut_settings (
    user_id     TEXT NOT NULL,
    symbol      TEXT NOT NULL REFERENCES fut_contracts(symbol),
    leverage    INT NOT NULL,
    margin_mode TEXT NOT NULL CHECK (margin_mode IN ('cross','isolated')),
    PRIMARY KEY (user_id, symbol)
);

CREATE TABLE fut_positions (
    user_id      TEXT NOT NULL,
    symbol       TEXT NOT NULL REFERENCES fut_contracts(symbol),
    qty          NUMERIC(36,18) NOT NULL DEFAULT 0,  -- signed: >0 long, <0 short
    entry_price  NUMERIC(36,18) NOT NULL DEFAULT 0,
    margin_mode  TEXT NOT NULL,
    leverage     INT NOT NULL,
    iso_margin   NUMERIC(36,18) NOT NULL DEFAULT 0,
    realized_pnl NUMERIC(36,18) NOT NULL DEFAULT 0,  -- since the position was opened
    tp_price     NUMERIC(36,18),
    sl_price     NUMERIC(36,18),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, symbol)
);

CREATE TABLE fut_orders (
    id            BIGSERIAL PRIMARY KEY,
    user_id       TEXT NOT NULL,
    symbol        TEXT NOT NULL REFERENCES fut_contracts(symbol),
    side          TEXT NOT NULL CHECK (side IN ('buy','sell')),
    type          TEXT NOT NULL CHECK (type IN ('limit','market')),
    tif           TEXT NOT NULL DEFAULT 'GTC',
    price         NUMERIC(36,18),
    qty           NUMERIC(36,18) NOT NULL,
    filled        NUMERIC(36,18) NOT NULL DEFAULT 0,
    filled_quote  NUMERIC(36,18) NOT NULL DEFAULT 0,
    reserved      NUMERIC(36,18) NOT NULL DEFAULT 0,
    fee           NUMERIC(36,18) NOT NULL DEFAULT 0,
    status        TEXT NOT NULL, -- new | partially_filled | filled | canceled
    reduce_only   BOOLEAN NOT NULL DEFAULT false,
    leverage      INT NOT NULL,
    margin_mode   TEXT NOT NULL,
    source        TEXT NOT NULL DEFAULT 'user', -- user | tp | sl | liquidation
    tp_price      NUMERIC(36,18),
    sl_price      NUMERIC(36,18),
    cancel_reason TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_orders_book ON fut_orders (symbol, side, price, id) WHERE status IN ('new','partially_filled') AND type = 'limit';
CREATE INDEX fut_orders_user ON fut_orders (user_id, id DESC);

CREATE TABLE fut_trades (
    id           BIGSERIAL PRIMARY KEY,
    symbol       TEXT NOT NULL,
    price        NUMERIC(36,18) NOT NULL,
    qty          NUMERIC(36,18) NOT NULL,
    taker_side   TEXT NOT NULL,
    maker_order  BIGINT NOT NULL,
    taker_order  BIGINT NOT NULL,
    maker_user   TEXT NOT NULL,
    taker_user   TEXT NOT NULL,
    maker_fee    NUMERIC(36,18) NOT NULL,
    taker_fee    NUMERIC(36,18) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_trades_sym ON fut_trades (symbol, id DESC);

-- Futures bills (money movements on fut_accounts / isolated margins).
CREATE TABLE fut_bills (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    symbol     TEXT,
    kind       TEXT NOT NULL, -- transfer_in | transfer_out | fee | realized_pnl | funding | liquidation_fee | insurance_cover | fee_income
    amount     NUMERIC(36,18) NOT NULL,
    ref        TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_bills_user ON fut_bills (user_id, id DESC);

-- Spot <-> futures transfers (outbox; the asset call is idempotent on ref).
CREATE TABLE fut_transfers (
    ref        TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    direction  TEXT NOT NULL CHECK (direction IN ('in','out')),
    amount     NUMERIC(36,18) NOT NULL,
    status     TEXT NOT NULL, -- pending | done | failed
    attempts   INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE fut_funding_history (
    symbol       TEXT NOT NULL,
    funding_time TIMESTAMPTZ NOT NULL,
    rate         NUMERIC(36,18) NOT NULL,
    mark_price   NUMERIC(36,18) NOT NULL,
    PRIMARY KEY (symbol, funding_time)
);

CREATE TABLE fut_mark_history (
    symbol      TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,
    index_price NUMERIC(36,18) NOT NULL,
    mark_price  NUMERIC(36,18) NOT NULL,
    PRIMARY KEY (symbol, ts)
);

CREATE TABLE fut_liquidations (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    symbol      TEXT NOT NULL,
    margin_mode TEXT NOT NULL,
    qty         NUMERIC(36,18) NOT NULL,
    mark_price  NUMERIC(36,18) NOT NULL,
    fee         NUMERIC(36,18) NOT NULL,
    shortfall   NUMERIC(36,18) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE fut_liquidations, fut_mark_history, fut_funding_history, fut_transfers, fut_bills,
  fut_trades, fut_orders, fut_positions, fut_settings, fut_accounts, fut_contracts;
