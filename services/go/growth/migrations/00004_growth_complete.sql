-- +goose Up
-- Growth completeness: task center, points shop, maker rebate, multi-level
-- referral commission. Points shop maintains a persistent user_points balance
-- that is independent of the limited-period /points/campaigns points.

-- Task center ------------------------------------------------------------
CREATE TABLE g_tasks (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT 'daily',
    condition_type TEXT NOT NULL,                    -- deposit|first_trade|invite|checkin|volume|kyc
    condition_value NUMERIC(36,8) NOT NULL DEFAULT 0, -- threshold (USDT for volume/deposit, count for invite)
    reward_kind   TEXT NOT NULL CHECK (reward_kind IN ('points','coupon_trial','coupon_deduction','asset')),
    reward_amount NUMERIC(36,8) NOT NULL,
    reward_asset  TEXT NOT NULL DEFAULT 'USDT',
    repeatable    BOOLEAN NOT NULL DEFAULT FALSE,
    daily         BOOLEAN NOT NULL DEFAULT FALSE,    -- claimable once per UTC day
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    sort          INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per (user, task, day). For non-daily tasks day is '1970-01-01' so
-- the unique index holds a single row.
CREATE TABLE g_user_tasks (
    uid         TEXT NOT NULL,
    task_id     TEXT NOT NULL REFERENCES g_tasks(id),
    day         DATE NOT NULL DEFAULT DATE '1970-01-01',
    progress    NUMERIC(36,8) NOT NULL DEFAULT 0,
    status      TEXT NOT NULL CHECK (status IN ('in_progress','claimable','claimed')),
    claimed_at  TIMESTAMPTZ,
    reward_ref  TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (uid, task_id, day)
);
CREATE INDEX g_user_tasks_status ON g_user_tasks (uid, status);

-- Seed baseline tasks. They are claimable as soon as the condition clears.
INSERT INTO g_tasks (id, title, description, category, condition_type, condition_value, reward_kind, reward_amount, reward_asset, repeatable, daily, sort) VALUES
  ('first_deposit',      '完成首次充值',            '向现货账户充值任意金额',       'newbie', 'deposit',    0,        'points',           50,  'USDT', FALSE, FALSE, 10),
  ('first_trade',        '完成首笔交易',            '任意品种任意数量',             'newbie', 'first_trade',0,        'points',           80,  'USDT', FALSE, FALSE, 20),
  ('kyc_basic',          '完成基础身份认证',        '通过 L1 身份认证',             'newbie', 'kyc',        1,        'points',           100, 'USDT', FALSE, FALSE, 30),
  ('invite_1',           '邀请 1 位好友',           '好友完成注册并通过 KYC',       'social', 'invite',     1,        'points',           100, 'USDT', FALSE, FALSE, 40),
  ('invite_5',           '邀请 5 位好友',           '累计邀请 5 位好友通过 KYC',    'social', 'invite',     5,        'points',           500, 'USDT', FALSE, FALSE, 41),
  ('daily_checkin',      '每日签到',                '每日签到领取积分',             'daily',  'checkin',    1,        'points',           5,   'USDT', TRUE,  TRUE,  50),
  ('daily_trade_vol',    '每日交易 100 USDT',       '当天累计交易量不少于 100 USDT','daily',  'volume',     100,      'points',           10,  'USDT', TRUE,  TRUE,  51),
  ('deposit_1000',       '累计充值 1,000 USDT',     '一次性或累计充值',             'growth', 'deposit',    1000,     'coupon_trial',     20,  'USDT', FALSE, FALSE, 60),
  ('volume_10000',       '合约累计交易 10,000 USDT','终身任务',                     'growth', 'volume',     10000,    'coupon_deduction', 10,  'USDT', FALSE, FALSE, 70);

-- Persistent points wallet (for the shop; independent of campaign points).
CREATE TABLE g_user_points (
    uid        TEXT PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    lifetime   BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE g_points_ledger (
    id         BIGSERIAL PRIMARY KEY,
    uid        TEXT NOT NULL,
    delta      BIGINT NOT NULL,                   -- positive earn, negative spend
    reason     TEXT NOT NULL,
    ref        TEXT NOT NULL,                     -- idempotency key
    balance    BIGINT NOT NULL,                   -- post-change balance
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (ref)
);
CREATE INDEX g_points_ledger_uid ON g_points_ledger (uid, at DESC);

-- Points shop ------------------------------------------------------------
CREATE TABLE g_shop_products (
    id          BIGSERIAL PRIMARY KEY,
    sku         TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    image       TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT 'physical',  -- physical | voucher | asset
    cost_points BIGINT NOT NULL CHECK (cost_points > 0),
    stock       INT NOT NULL DEFAULT 0,            -- -1 = unlimited
    reward_kind TEXT NOT NULL DEFAULT 'physical' CHECK (reward_kind IN ('physical','coupon_trial','coupon_deduction','asset')),
    reward_amount NUMERIC(36,8) NOT NULL DEFAULT 0,
    reward_asset  TEXT NOT NULL DEFAULT '',
    daily_limit INT NOT NULL DEFAULT 0,            -- 0 = unlimited per day
    user_limit  INT NOT NULL DEFAULT 0,            -- 0 = unlimited per user
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    sort        INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO g_shop_products (sku, name, description, category, cost_points, stock, reward_kind, reward_amount, reward_asset, user_limit, sort) VALUES
  ('coupon-5',   '5 USDT 手续费抵扣券',   '合约手续费抵扣，7 天有效',   'voucher', 50,   -1, 'coupon_deduction', 5,   'USDT', 0,  10),
  ('coupon-20',  '20 USDT 体验金',        '合约体验金，14 天有效',     'voucher', 200,  -1, 'coupon_trial',     20,  'USDT', 0,  20),
  ('coupon-50',  '50 USDT 体验金',        '合约体验金，14 天有效',     'voucher', 500,  -1, 'coupon_trial',     50,  'USDT', 3,  30),
  ('coupon-100', '100 USDT 抵扣金',       '合约手续费抵扣',             'voucher', 1000, -1, 'coupon_deduction', 100, 'USDT', 2,  40),
  ('merch-tee',  'Linbit 限量 T 恤',      '含运费，仅限中国大陆地区',   'physical',1500, 50, 'physical',         0,   '',     1,  50),
  ('merch-mug',  'Linbit 保温杯',         '含运费，仅限中国大陆地区',   'physical',800,  200,'physical',         0,   '',     1,  60),
  ('asset-5',    '直接发放 5 USDT',       '即刻到账现货账户',           'asset',   800,  500,'asset',            5,   'USDT', 0,  70);

CREATE TABLE g_shop_orders (
    id            BIGSERIAL PRIMARY KEY,
    uid           TEXT NOT NULL,
    product_id    BIGINT NOT NULL REFERENCES g_shop_products(id),
    sku           TEXT NOT NULL,
    quantity      INT NOT NULL CHECK (quantity > 0),
    cost_points   BIGINT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('pending','shipped','delivered','cancelled')),
    shipping_name TEXT NOT NULL DEFAULT '',
    shipping_phone TEXT NOT NULL DEFAULT '',
    shipping_addr TEXT NOT NULL DEFAULT '',
    tracking      TEXT NOT NULL DEFAULT '',
    reward_ref    TEXT NOT NULL DEFAULT '',
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX g_shop_orders_uid ON g_shop_orders (uid, created_at DESC);

-- Maker rebate -----------------------------------------------------------
-- Role fills: g_fee_events.copy was repurposed earlier; add explicit role.
ALTER TABLE g_fee_events ADD COLUMN role TEXT NOT NULL DEFAULT '';
UPDATE g_fee_events SET role='taker';
CREATE INDEX g_fee_events_maker ON g_fee_events (uid, at) WHERE role='maker';

-- VIP-level rebate rules (keyed by vip_level). Rate is applied to the maker
-- fee as additional rebate to the user.
CREATE TABLE g_rebate_rules (
    vip_level   INT PRIMARY KEY,
    name        TEXT NOT NULL,
    maker_rebate NUMERIC(10,6) NOT NULL,            -- fraction of maker fee refunded
    min_volume  NUMERIC(36,8) NOT NULL DEFAULT 0,   -- 30d maker volume threshold
    enabled     BOOLEAN NOT NULL DEFAULT TRUE
);
INSERT INTO g_rebate_rules (vip_level, name, maker_rebate, min_volume) VALUES
  (0,  'VIP 0',  0.000000, 0),
  (1,  'VIP 1',  0.050000, 50000),
  (2,  'VIP 2',  0.100000, 250000),
  (3,  'VIP 3',  0.150000, 1000000),
  (4,  'VIP 4',  0.200000, 5000000),
  (5,  'VIP 5',  0.250000, 20000000),
  (6,  'VIP 6',  0.300000, 50000000),
  (7,  'VIP 7',  0.350000, 100000000),
  (8,  'VIP 8',  0.400000, 250000000),
  (9,  'VIP 9',  0.450000, 500000000),
  (10, 'VIP 10', 0.500000, 1000000000);

CREATE TABLE g_rebate_accruals (
    id          BIGSERIAL PRIMARY KEY,
    uid         TEXT NOT NULL,
    period      DATE NOT NULL,
    vip_level   INT NOT NULL,
    maker_volume NUMERIC(36,8) NOT NULL DEFAULT 0,
    maker_fee   NUMERIC(36,12) NOT NULL DEFAULT 0,
    rebate_rate NUMERIC(10,6) NOT NULL,
    amount      NUMERIC(36,12) NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('pending','paid')),
    paid_at     TIMESTAMPTZ,
    UNIQUE (uid, period)
);

-- Multi-level referral ---------------------------------------------------
-- L1/L2/L3 rates on fee base. The first non-null inviter chain is walked up
-- to three levels. Keeping rates in a table makes them hot-reloadable.
CREATE TABLE g_referral_tiers (
    level INT PRIMARY KEY,
    rate  NUMERIC(10,6) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);
INSERT INTO g_referral_tiers (level, rate) VALUES
  (1, 0.300000),
  (2, 0.100000),
  (3, 0.050000);

-- Withdrawals of accumulated referral commission to spot (funding).
CREATE TABLE g_referral_withdrawals (
    id         BIGSERIAL PRIMARY KEY,
    uid        TEXT NOT NULL,
    amount     NUMERIC(36,8) NOT NULL CHECK (amount > 0),
    asset      TEXT NOT NULL DEFAULT 'USDT',
    ref        TEXT NOT NULL UNIQUE,
    status     TEXT NOT NULL CHECK (status IN ('pending','paid','failed')),
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at    TIMESTAMPTZ
);
CREATE INDEX g_referral_withdrawals_uid ON g_referral_withdrawals (uid, at DESC);

-- Daily checkins (stores streak; each checkin also credits task progress).
CREATE TABLE g_checkins (
    uid    TEXT NOT NULL,
    day    DATE NOT NULL,
    streak INT NOT NULL,
    PRIMARY KEY (uid, day)
);

-- +goose Down
DROP TABLE g_checkins;
DROP TABLE g_referral_withdrawals;
DROP TABLE g_referral_tiers;
DROP TABLE g_rebate_accruals;
DROP TABLE g_rebate_rules;
DROP INDEX IF EXISTS g_fee_events_maker;
ALTER TABLE g_fee_events DROP COLUMN role;
DROP TABLE g_shop_orders;
DROP TABLE g_shop_products;
DROP INDEX IF EXISTS g_points_ledger_uid;
DROP TABLE g_points_ledger;
DROP TABLE g_user_points;
DROP INDEX IF EXISTS g_user_tasks_status;
DROP TABLE g_user_tasks;
DROP TABLE g_tasks;
