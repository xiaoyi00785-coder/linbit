-- +goose Up
CREATE TABLE grid_bots (
    id            BIGSERIAL PRIMARY KEY,
    user_id       TEXT NOT NULL,
    symbol        TEXT NOT NULL,
    base_asset    TEXT NOT NULL,
    quote_asset   TEXT NOT NULL,
    mode          TEXT NOT NULL CHECK (mode IN ('arithmetic','geometric')),
    lower_price   NUMERIC(36,18) NOT NULL,
    upper_price   NUMERIC(36,18) NOT NULL,
    grids         INT NOT NULL,
    investment    NUMERIC(36,18) NOT NULL,
    qty_per_grid  NUMERIC(36,18) NOT NULL,
    stop_loss     NUMERIC(36,18),
    take_profit   NUMERIC(36,18),
    sell_on_stop  BOOLEAN NOT NULL DEFAULT TRUE,
    start_price   NUMERIC(36,18) NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('starting','running','stopping','stopped')),
    stop_reason   TEXT,
    base_qty      NUMERIC(36,18) NOT NULL DEFAULT 0,
    quote_bal     NUMERIC(36,18) NOT NULL DEFAULT 0,
    grid_profit   NUMERIC(36,18) NOT NULL DEFAULT 0,
    fees_quote    NUMERIC(36,18) NOT NULL DEFAULT 0,
    fees_base     NUMERIC(36,18) NOT NULL DEFAULT 0,
    cycles        INT NOT NULL DEFAULT 0,
    peak_value    NUMERIC(36,18) NOT NULL DEFAULT 0,
    max_drawdown  NUMERIC(36,18) NOT NULL DEFAULT 0,
    final_value   NUMERIC(36,18),
    final_price   NUMERIC(36,18),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at    TIMESTAMPTZ
);
CREATE INDEX grid_bots_user ON grid_bots (user_id, id DESC);
CREATE INDEX grid_bots_active ON grid_bots (status) WHERE status <> 'stopped';

CREATE TABLE grid_orders (
    id           BIGSERIAL PRIMARY KEY,
    bot_id       BIGINT NOT NULL REFERENCES grid_bots(id),
    level        INT NOT NULL,          -- -1 = start/stop market order
    side         TEXT NOT NULL CHECK (side IN ('buy','sell')),
    kind         TEXT NOT NULL CHECK (kind IN ('grid','init','close')),
    price        NUMERIC(36,18),
    quantity     NUMERIC(36,18) NOT NULL,
    cost         NUMERIC(36,18) NOT NULL DEFAULT 0,   -- sell: cost basis of quantity
    order_id     TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','filled','canceled')),
    filled_qty   NUMERIC(36,18) NOT NULL DEFAULT 0,
    filled_quote NUMERIC(36,18) NOT NULL DEFAULT 0,
    fee_base     NUMERIC(36,18) NOT NULL DEFAULT 0,
    fee_quote    NUMERIC(36,18) NOT NULL DEFAULT 0,
    profit       NUMERIC(36,18),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at      TIMESTAMPTZ
);
CREATE INDEX grid_orders_open ON grid_orders (bot_id) WHERE status = 'open';
CREATE INDEX grid_orders_bot ON grid_orders (bot_id, id DESC);

CREATE TABLE grid_snapshots (
    bot_id BIGINT NOT NULL REFERENCES grid_bots(id),
    at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    value  NUMERIC(36,18) NOT NULL,
    price  NUMERIC(36,18) NOT NULL
);
CREATE INDEX grid_snapshots_bot ON grid_snapshots (bot_id, at);

-- +goose Down
DROP TABLE grid_snapshots;
DROP TABLE grid_orders;
DROP TABLE grid_bots;
