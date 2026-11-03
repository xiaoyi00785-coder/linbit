-- +goose Up
-- Market-page category tabs. ids match apps/web market chips; names are the
-- displayed labels. A symbol may sit in several categories. Categories with no
-- listed symbols (metals, commodities, stocks) return an empty list.
CREATE TABLE market_categories (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    sort       INT  NOT NULL DEFAULT 0,
    symbols    TEXT[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO market_categories (id, name, sort, symbols) VALUES
    ('layer1',      '公链',     10, '{BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,TRXUSDT}'),
    ('metals',      '贵金属',   20, '{}'),
    ('commodities', '大宗商品', 30, '{}'),
    ('stocks',      '股票',     40, '{}'),
    ('meme',        'Meme',     50, '{}'),
    ('ai',          'AI',       60, '{}'),
    ('defi',        'DeFi',     70, '{}'),
    ('layer2',      'Layer 2',  80, '{}'),
    ('ecosystem',   '生态',     90, '{BNBUSDT,TRXUSDT,SOLUSDT}');

-- +goose Down
DROP TABLE market_categories;
