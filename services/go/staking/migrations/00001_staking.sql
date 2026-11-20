-- +goose Up
-- On-chain PoS staking. Account-based (no live chain client); each product
-- pins one asset to one validator with its own APR, unbonding period and
-- minimum stake. Users stake out of the spot ledger, earn a daily reward
-- while the position is active, and get principal + any unclaimed rewards
-- back on the spot ledger once an unstake finishes unbonding.
CREATE TABLE s_products (
    id              TEXT PRIMARY KEY,
    asset           TEXT NOT NULL,
    chain           TEXT NOT NULL,                      -- display: ETH, Solana, Cosmos, ...
    validator       TEXT NOT NULL,                      -- display validator name
    validator_addr  TEXT,                               -- optional on-chain address
    apr             NUMERIC(10,6) NOT NULL,             -- 0.05 = 5%
    unbonding_days  INT  NOT NULL DEFAULT 0,
    min_amount      NUMERIC(36,8) NOT NULL,
    max_amount      NUMERIC(36,8),                      -- per-user cap on open positions
    total_quota     NUMERIC(36,8),                      -- NULL = unlimited
    sold            NUMERIC(36,8) NOT NULL DEFAULT 0,
    commission      NUMERIC(10,6) NOT NULL DEFAULT 0,   -- validator take, informational
    logo_url        TEXT,
    status          TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','offline')),
    hot             BOOLEAN NOT NULL DEFAULT false,
    sort            INT NOT NULL DEFAULT 0,
    UNIQUE (asset, validator)
);
CREATE INDEX s_products_online ON s_products (status, sort);

-- One row per stake. Partial unstakes close the old row's remainder into a
-- new 'active' row so the unbonding timer tracks exactly the amount leaving.
CREATE TABLE s_positions (
    id               BIGSERIAL PRIMARY KEY,
    uid              TEXT NOT NULL,
    product_id       TEXT NOT NULL REFERENCES s_products(id),
    asset            TEXT NOT NULL,
    amount           NUMERIC(36,8) NOT NULL,
    apr_snapshot     NUMERIC(10,6) NOT NULL,            -- APR locked at stake time
    status           TEXT NOT NULL CHECK (status IN ('active','unbonding','released','cancelled')),
    started_at       TIMESTAMPTZ NOT NULL,
    unbond_at        TIMESTAMPTZ,                       -- when unbonding completes
    released_at      TIMESTAMPTZ,
    last_settle_day  INT NOT NULL,                      -- last UTC day index whose reward was written
    pending_rewards  NUMERIC(36,8) NOT NULL DEFAULT 0,  -- accrued, not yet claimed
    claimed_rewards  NUMERIC(36,8) NOT NULL DEFAULT 0
);
CREATE INDEX s_positions_uid ON s_positions (uid, status);
CREATE INDEX s_positions_due ON s_positions (unbond_at) WHERE status = 'unbonding';
CREATE INDEX s_positions_active ON s_positions (status, last_settle_day) WHERE status IN ('active','unbonding');

-- One row per position per day the reward settlement runs; deterministic so a
-- retried tick never credits the same day twice.
CREATE TABLE s_rewards (
    id          BIGSERIAL PRIMARY KEY,
    position_id BIGINT NOT NULL REFERENCES s_positions(id),
    uid         TEXT NOT NULL,
    asset       TEXT NOT NULL,
    epoch       INT NOT NULL,                           -- UTC day index
    amount      NUMERIC(36,8) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    claimed_at  TIMESTAMPTZ,
    UNIQUE (position_id, epoch)
);
CREATE INDEX s_rewards_uid ON s_rewards (uid, created_at DESC);
CREATE INDEX s_rewards_unclaimed ON s_rewards (uid, claimed_at) WHERE claimed_at IS NULL;

-- Human-readable history: stake / unstake / claim / release.
CREATE TABLE s_events (
    id          BIGSERIAL PRIMARY KEY,
    uid         TEXT NOT NULL,
    position_id BIGINT,
    product_id  TEXT,
    kind        TEXT NOT NULL CHECK (kind IN ('stake','unstake','claim','release','reward','cancel')),
    asset       TEXT NOT NULL,
    amount      NUMERIC(36,8) NOT NULL,
    detail      JSONB NOT NULL DEFAULT '{}',
    at          TIMESTAMPTZ NOT NULL
);
CREATE INDEX s_events_uid ON s_events (uid, at DESC);

-- Seed catalogue; ops can edit via POST /internal/staking/products.
INSERT INTO s_products (id, asset, chain, validator, validator_addr, apr, unbonding_days, min_amount, max_amount, commission, hot, sort) VALUES
 ('stk-ETH-lido',       'ETH',  'Ethereum', 'Lido',        '0xae7ab96520de3a18e5e111b5eaab095312d7fe84', 0.032, 7,  0.01,  NULL, 0.10, true,  10),
 ('stk-ETH-rocketpool', 'ETH',  'Ethereum', 'Rocket Pool', '0xae78736cd615f374d3085123a210448e74fc6393', 0.030, 7,  0.01,  NULL, 0.15, false, 11),
 ('stk-SOL-jito',       'SOL',  'Solana',   'Jito',        'J1toso1uCk3RLmjorhTtrVwY9HJ7X8V9yYac6Y7kGCPn', 0.072, 3,  0.1,   NULL, 0.05, true,  20),
 ('stk-SOL-marinade',   'SOL',  'Solana',   'Marinade',    '8szGkuLTAux9XMgZ2vtY39jVSowEcpBfFfD8hXSEqdGC', 0.068, 3,  0.1,   NULL, 0.06, false, 21),
 ('stk-ATOM-cosmos',    'ATOM', 'Cosmos',   'Cosmoshub Validator 1', 'cosmosvaloper156gqf9837u7d4c4678yt3rl4ls9c5vuursrrzf', 0.142, 21, 0.1,  NULL, 0.05, true,  30),
 ('stk-DOT-polkadot',   'DOT',  'Polkadot', 'Polkadot Nominator #1', NULL,                                                   0.118, 28, 1,    NULL, 0.03, false, 40),
 ('stk-ADA-cardano',    'ADA',  'Cardano',  'IOHK Pool',   NULL,                                                             0.028,  0, 10,   NULL, 0.00, false, 50),
 ('stk-TRX-tron',       'TRX',  'Tron',     'Tron SR #1',  NULL,                                                             0.044, 14, 10,   NULL, 0.00, false, 60),
 ('stk-AVAX-avalanche', 'AVAX', 'Avalanche','Avalanche Validator #1', NULL,                                                   0.078, 14, 25,   NULL, 0.02, false, 70),
 ('stk-NEAR-near',      'NEAR', 'NEAR',     'Figment',     NULL,                                                             0.091,  3, 1,    NULL, 0.05, false, 80);

-- +goose Down
DROP TABLE s_events;
DROP TABLE s_rewards;
DROP TABLE s_positions;
DROP TABLE s_products;
