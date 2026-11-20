-- +goose Up
-- VIP tier system (equity/volume-based levels, distinct from the manual
-- VIP concierge application in vip_applications). The user's level is
-- recomputed hourly from their 30-day trading volume and current equity
-- against vip_tiers; order/futures read maker/taker rates from this level.
CREATE TABLE vip_tiers (
    level                INT PRIMARY KEY,
    name                 TEXT NOT NULL,
    min_volume_30d_usdt  NUMERIC(36,8) NOT NULL DEFAULT 0,
    min_asset_usdt       NUMERIC(36,8) NOT NULL DEFAULT 0,
    maker_rate           NUMERIC(10,6) NOT NULL,
    taker_rate           NUMERIC(10,6) NOT NULL,
    futures_maker_rate   NUMERIC(10,6) NOT NULL,
    futures_taker_rate   NUMERIC(10,6) NOT NULL,
    withdrawal_free_cnt  INT NOT NULL DEFAULT 0,
    perks                JSONB NOT NULL DEFAULT '[]'::jsonb
);

INSERT INTO vip_tiers (level, name, min_volume_30d_usdt, min_asset_usdt,
    maker_rate, taker_rate, futures_maker_rate, futures_taker_rate,
    withdrawal_free_cnt, perks) VALUES
 (0,  'VIP 0',   0,          0,       0.001000, 0.001000, 0.000200, 0.000500, 0,
    '["基础费率"]'),
 (1,  'VIP 1',   50000,      1000,    0.000900, 0.001000, 0.000180, 0.000450, 1,
    '["9 折现货手续费","1 次免手续费提币"]'),
 (2,  'VIP 2',   250000,     5000,    0.000800, 0.000900, 0.000160, 0.000420, 2,
    '["人工客服加速","手续费 8 折"]'),
 (3,  'VIP 3',   1000000,    20000,   0.000700, 0.000800, 0.000140, 0.000400, 3,
    '["专属客户经理","提款绿色通道"]'),
 (4,  'VIP 4',   5000000,    50000,   0.000600, 0.000700, 0.000120, 0.000380, 5,
    '["做市商入门费率","活动优先资格"]'),
 (5,  'VIP 5',   20000000,   200000,  0.000500, 0.000600, 0.000100, 0.000360, 8,
    '["每月生日礼","社群优先权"]'),
 (6,  'VIP 6',   50000000,   500000,  0.000400, 0.000500, 0.000080, 0.000340, 12,
    '["定制礼品","赛事活动优先"]'),
 (7,  'VIP 7',   100000000,  1000000, 0.000300, 0.000400, 0.000060, 0.000320, 20,
    '["市场研报推送","OTC 优先成交"]'),
 (8,  'VIP 8',   250000000,  2500000, 0.000200, 0.000300, 0.000040, 0.000300, 35,
    '["做市商佣金返还","VVIP 社群"]'),
 (9,  'VIP 9',   500000000,  5000000, 0.000100, 0.000200, 0.000020, 0.000280, 60,
    '["行业大会邀请","定制 API 支持"]'),
 (10, 'VIP 10',  1000000000, 10000000,0.000000, 0.000100, 0.000000, 0.000250, 999,
    '["0 现货 maker 手续费","专属上币通道"]');

-- Cached tier per user. Updated by the hourly recompute worker using the
-- published asset valuation and 30-day growth volume. Default is tier 0;
-- the row is implicit (if missing we treat the user as VIP 0).
CREATE TABLE user_vip_levels (
    user_id       BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    uid           TEXT NOT NULL,
    level         INT NOT NULL REFERENCES vip_tiers(level),
    volume_30d    NUMERIC(36,8) NOT NULL DEFAULT 0,
    assets_usdt   NUMERIC(36,8) NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_vip_levels_updated ON user_vip_levels (updated_at);

-- +goose Down
DROP TABLE user_vip_levels;
DROP TABLE vip_tiers;
