-- +goose Up
-- Virtual USDX perpetual account. No foreign keys to live ledgers or fut_accounts.
CREATE TABLE paper_fut_accounts (
    user_id        TEXT PRIMARY KEY,
    balance        NUMERIC(36,18) NOT NULL DEFAULT 10000 CHECK (balance >= 0),
    frozen         NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (frozen >= 0),
    position_mode  TEXT NOT NULL DEFAULT 'one_way' CHECK (position_mode IN ('one_way','hedge')),
    generation     BIGINT NOT NULL DEFAULT 1 CHECK (generation > 0),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE paper_fut_settings (
    user_id      TEXT NOT NULL REFERENCES paper_fut_accounts(user_id),
    symbol       TEXT NOT NULL,
    leverage     INT NOT NULL CHECK (leverage >= 1),
    margin_mode  TEXT NOT NULL CHECK (margin_mode IN ('cross','isolated')),
    PRIMARY KEY (user_id, symbol)
);

CREATE TABLE paper_fut_positions (
    user_id          TEXT NOT NULL,
    symbol           TEXT NOT NULL,
    position_side    TEXT NOT NULL CHECK (position_side IN ('BOTH','LONG','SHORT')),
    margin_mode      TEXT NOT NULL CHECK (margin_mode IN ('cross','isolated')),
    leverage         INT NOT NULL CHECK (leverage >= 1),
    qty              NUMERIC(36,18) NOT NULL,
    entry_price      NUMERIC(36,18) NOT NULL DEFAULT 0,
    isolated_margin  NUMERIC(36,18) NOT NULL DEFAULT 0 CHECK (isolated_margin >= 0),
    realized_pnl     NUMERIC(36,18) NOT NULL DEFAULT 0,
    tp_trigger       NUMERIC(36,18),
    sl_trigger       NUMERIC(36,18),
    version          BIGINT NOT NULL DEFAULT 1,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, symbol, position_side)
);

CREATE TABLE paper_fut_orders (
    order_id        TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    client_order_id TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy','sell')),
    position_side   TEXT NOT NULL,
    action          TEXT NOT NULL CHECK (action IN ('open','close')),
    order_type      TEXT NOT NULL CHECK (order_type IN ('market','limit','plan','post_only')),
    price           NUMERIC(36,18),
    trigger_price   NUMERIC(36,18),
    qty             NUMERIC(36,18) NOT NULL CHECK (qty > 0),
    filled_qty      NUMERIC(36,18) NOT NULL DEFAULT 0,
    leverage        INT NOT NULL,
    margin_mode     TEXT NOT NULL,
    status          TEXT NOT NULL,
    reduce_only     BOOLEAN NOT NULL DEFAULT FALSE,
    tp_trigger      NUMERIC(36,18),
    sl_trigger      NUMERIC(36,18),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, client_order_id)
);
CREATE INDEX paper_fut_orders_open ON paper_fut_orders (user_id, status, created_at DESC);

CREATE TABLE paper_fut_trades (
    trade_id     TEXT PRIMARY KEY,
    order_id     TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    symbol       TEXT NOT NULL,
    side         TEXT NOT NULL,
    price        NUMERIC(36,18) NOT NULL,
    qty          NUMERIC(36,18) NOT NULL,
    realized_pnl NUMERIC(36,18) NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE paper_fut_ledger (
    id            BIGSERIAL PRIMARY KEY,
    user_id       TEXT NOT NULL,
    kind          TEXT NOT NULL,
    amount        NUMERIC(36,18) NOT NULL,
    balance_after NUMERIC(36,18) NOT NULL,
    ref           TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, ref)
);
CREATE INDEX paper_fut_ledger_user ON paper_fut_ledger (user_id, id DESC);

CREATE TABLE paper_fut_claims (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    amount     NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per liquidation/funding application. The primary key is the idempotency key.
CREATE TABLE paper_fut_events (
    idempotency_key TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    kind            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE paper_fut_events;
DROP TABLE paper_fut_claims;
DROP TABLE paper_fut_ledger;
DROP TABLE paper_fut_trades;
DROP TABLE paper_fut_orders;
DROP TABLE paper_fut_positions;
DROP TABLE paper_fut_settings;
DROP TABLE paper_fut_accounts;
