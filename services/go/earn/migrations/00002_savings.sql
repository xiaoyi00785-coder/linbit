-- +goose Up
-- Savings products: simple (flexible, tiered APR), fixed term (理财宝) and
-- on-chain staking (with an unlock period on redemption).
CREATE TABLE e_products (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('simple','fixed','onchain')),
    asset         TEXT NOT NULL,
    title         TEXT NOT NULL,
    apr           NUMERIC(10,6) NOT NULL,          -- base APR (0.012 = 1.2%)
    bonus_apr     NUMERIC(10,6),                   -- simple: APR on the first bonus_quota
    bonus_quota   NUMERIC(36,8),
    term_days     INT NOT NULL DEFAULT 0,
    unlock_days   INT NOT NULL DEFAULT 0,
    min_amount    NUMERIC(36,8) NOT NULL,
    per_user_max  NUMERIC(36,8),
    total_quota   NUMERIC(36,8),
    sold          NUMERIC(36,8) NOT NULL DEFAULT 0,
    newcomer      BOOLEAN NOT NULL DEFAULT false,
    hot           BOOLEAN NOT NULL DEFAULT false,
    status        TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','offline')),
    sort          INT NOT NULL DEFAULT 0
);

-- simple / onchain holdings: one row per user and product.
CREATE TABLE e_holdings (
    id              BIGSERIAL PRIMARY KEY,
    uid             TEXT NOT NULL,
    product_id      TEXT NOT NULL REFERENCES e_products(id),
    principal       NUMERIC(36,8) NOT NULL DEFAULT 0, -- earning
    pending         NUMERIC(36,8) NOT NULL DEFAULT 0, -- subscribed today, earns from tomorrow
    pending_day     INT NOT NULL DEFAULT 0,
    last_day        INT NOT NULL,                     -- last UTC day index settled
    interest_total  NUMERIC(36,8) NOT NULL DEFAULT 0,
    auto            BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (uid, product_id)
);

CREATE TABLE e_fixed_orders (
    id           BIGSERIAL PRIMARY KEY,
    uid          TEXT NOT NULL,
    product_id   TEXT NOT NULL REFERENCES e_products(id),
    asset        TEXT NOT NULL,
    amount       NUMERIC(36,8) NOT NULL,
    apr          NUMERIC(10,6) NOT NULL,
    term_days    INT NOT NULL,
    interest     NUMERIC(36,8) NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('pending','holding','matured','failed')),
    start_at     TIMESTAMPTZ NOT NULL,
    maturity_at  TIMESTAMPTZ NOT NULL,
    settled_at   TIMESTAMPTZ
);
CREATE INDEX e_fixed_orders_uid ON e_fixed_orders (uid, status);
CREATE INDEX e_fixed_orders_due ON e_fixed_orders (maturity_at) WHERE status = 'holding';

CREATE TABLE e_unlocks (
    id          BIGSERIAL PRIMARY KEY,
    uid         TEXT NOT NULL,
    product_id  TEXT NOT NULL REFERENCES e_products(id),
    asset       TEXT NOT NULL,
    amount      NUMERIC(36,8) NOT NULL,
    release_at  TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('unlocking','released')),
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX e_unlocks_due ON e_unlocks (release_at) WHERE status = 'unlocking';

CREATE TABLE e_earn_events (
    id          BIGSERIAL PRIMARY KEY,
    uid         TEXT NOT NULL,
    product_id  TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('subscribe','redeem','interest','maturity','unlock')),
    asset       TEXT NOT NULL,
    amount      NUMERIC(36,8) NOT NULL,
    detail      JSONB NOT NULL DEFAULT '{}',
    at          TIMESTAMPTZ NOT NULL
);
CREATE INDEX e_earn_events_uid ON e_earn_events (uid, at DESC);

-- Seed catalogue (ops can change it via POST /internal/earn/products).
INSERT INTO e_products (id, kind, asset, title, apr, bonus_apr, bonus_quota, term_days, unlock_days, min_amount, per_user_max, total_quota, newcomer, hot, sort) VALUES
 ('simple-USDT','simple','USDT','USDT 活期',0.010,0.012,10000,0,0,1,NULL,NULL,false,false,10),
 ('simple-USDC','simple','USDC','USDC 活期',0.010,0.011,10000,0,0,1,NULL,NULL,false,false,11),
 ('simple-BTC','simple','BTC','BTC 活期',0.003,0.005,0.5,0,0,0.0001,NULL,NULL,false,false,12),
 ('simple-ETH','simple','ETH','ETH 活期',0.008,0.014,5,0,0,0.001,NULL,NULL,false,false,13),
 ('simple-SOL','simple','SOL','SOL 活期',0.0183,0.019,100,0,0,0.01,NULL,NULL,false,false,14),
 ('simple-DOGE','simple','DOGE','DOGE 活期',0.006,0.010,50000,0,0,10,NULL,NULL,false,false,15),
 ('fixed-USDT-7','fixed','USDT','理财宝 | 7 天',0.088,NULL,NULL,7,0,10,1000,NULL,true,true,20),
 ('fixed-USDT-30','fixed','USDT','理财宝 | 30 天',0.046,NULL,NULL,30,0,10,NULL,NULL,false,false,21),
 ('fixed-BTC-90','fixed','BTC','理财宝 | 90 天',0.019,NULL,NULL,90,0,0.0001,NULL,NULL,false,false,22),
 ('fixed-ETH-7','fixed','ETH','理财宝 | 7 天',0.052,NULL,NULL,7,0,0.001,NULL,NULL,false,true,23),
 ('fixed-ETH-60','fixed','ETH','理财宝 | 60 天',0.032,NULL,NULL,60,0,0.001,NULL,NULL,false,false,24),
 ('fixed-SOL-14','fixed','SOL','理财宝 | 14 天',0.061,NULL,NULL,14,0,0.01,NULL,NULL,false,false,25),
 ('fixed-USDC-180','fixed','USDC','理财宝 | 180 天',0.050,NULL,NULL,180,0,10,NULL,NULL,false,false,26),
 ('fixed-TRX-365','fixed','TRX','理财宝 | 365 天',0.059,NULL,NULL,365,0,10,NULL,NULL,false,false,27),
 ('chain-ETH','onchain','ETH','ETH 质押',0.029,NULL,NULL,0,0,0.001,NULL,NULL,false,false,30),
 ('chain-SOL','onchain','SOL','SOL 质押',0.064,NULL,NULL,0,2,0.01,NULL,NULL,false,true,31),
 ('chain-TRX','onchain','TRX','TRX 质押',0.041,NULL,NULL,0,14,10,NULL,NULL,false,false,32),
 ('chain-DOT','onchain','DOT','DOT 质押',0.112,NULL,NULL,0,28,1,NULL,NULL,false,false,33),
 ('chain-ATOM','onchain','ATOM','ATOM 质押',0.135,NULL,NULL,0,21,0.1,NULL,NULL,false,false,34),
 ('chain-ADA','onchain','ADA','ADA 质押',0.026,NULL,NULL,0,0,10,NULL,NULL,false,false,35);

-- +goose Down
DROP TABLE e_earn_events;
DROP TABLE e_unlocks;
DROP TABLE e_fixed_orders;
DROP TABLE e_holdings;
DROP TABLE e_products;
