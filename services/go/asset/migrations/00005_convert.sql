-- +goose Up
-- 闪兑 (convert): RFQ quotes against the platform (counterparty system:convert).
CREATE TABLE convert_config (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    spread_bps INT NOT NULL,
    min_usdt   NUMERIC(36,18) NOT NULL,
    max_usdt   NUMERIC(36,18) NOT NULL,
    updated_by TEXT NOT NULL DEFAULT 'default',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE convert_assets (
    asset      TEXT PRIMARY KEY,
    precision  INT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    sort       INT NOT NULL DEFAULT 0
);
INSERT INTO convert_assets (asset, precision, sort) VALUES
  ('USDT',6,1),('USDC',6,2),('BTC',8,3),('ETH',8,4),('SOL',6,5),('BNB',8,6),('TRX',6,7);
CREATE TABLE convert_quotes (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    from_asset  TEXT NOT NULL,
    to_asset    TEXT NOT NULL,
    from_amount NUMERIC(36,18) NOT NULL,
    to_amount   NUMERIC(36,18) NOT NULL,
    rate        NUMERIC(36,18) NOT NULL,
    index_rate  NUMERIC(36,18) NOT NULL,
    spread_bps  INT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open', -- open | filled
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX convert_quotes_user ON convert_quotes (user_id, created_at DESC);
CREATE TABLE convert_orders (
    id          BIGSERIAL PRIMARY KEY,
    quote_id    TEXT NOT NULL UNIQUE REFERENCES convert_quotes(id),
    user_id     TEXT NOT NULL,
    from_asset  TEXT NOT NULL,
    to_asset    TEXT NOT NULL,
    from_amount NUMERIC(36,18) NOT NULL,
    to_amount   NUMERIC(36,18) NOT NULL,
    rate        NUMERIC(36,18) NOT NULL,
    index_rate  NUMERIC(36,18) NOT NULL,
    spread_bps  INT NOT NULL,
    usdt_value  NUMERIC(36,18) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX convert_orders_user ON convert_orders (user_id, id DESC);

-- +goose Down
DROP TABLE convert_orders;
DROP TABLE convert_quotes;
DROP TABLE convert_assets;
DROP TABLE convert_config;
