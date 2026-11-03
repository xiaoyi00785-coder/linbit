-- +goose Up

-- Per-user spot fee overrides (NULL = use the global rate).
CREATE TABLE spot_fee_overrides (
    user_id    TEXT PRIMARY KEY,
    maker      NUMERIC(10,8) CHECK (maker IS NULL OR (maker >= 0 AND maker < 0.1)),
    taker      NUMERIC(10,8) CHECK (taker IS NULL OR (taker >= 0 AND taker < 0.1)),
    note       TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Conditional (stop / take-profit / trigger) spot orders. Funds are frozen with
-- ref cond:<id> on creation and released when the order fires or is canceled.
CREATE TABLE spot_conditional_orders (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy','sell')),
    kind            TEXT NOT NULL CHECK (kind IN ('stop','take_profit','trigger')),
    order_type      TEXT NOT NULL CHECK (order_type IN ('limit','market')),
    trigger_price   NUMERIC(36,18) NOT NULL,
    direction       TEXT NOT NULL CHECK (direction IN ('up','down')),
    price           NUMERIC(36,18),
    quantity        NUMERIC(36,18),
    quote_qty       NUMERIC(36,18),
    tif             TEXT NOT NULL DEFAULT 'GTC',
    client_order_id TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','triggering','triggered','canceled','failed')),
    order_id        TEXT,
    frozen_asset    TEXT NOT NULL,
    frozen_amount   NUMERIC(36,18) NOT NULL,
    fail_reason     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    triggered_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX spot_cond_pending ON spot_conditional_orders (symbol) WHERE status IN ('pending','triggering');
CREATE INDEX spot_cond_user ON spot_conditional_orders (user_id, created_at DESC);
CREATE UNIQUE INDEX spot_cond_client ON spot_conditional_orders (user_id, client_order_id) WHERE client_order_id IS NOT NULL;

-- +goose Down
DROP TABLE spot_conditional_orders;
DROP TABLE spot_fee_overrides;
