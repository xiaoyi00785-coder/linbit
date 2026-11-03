-- +goose Up
-- Coupon center (卡券中心): trial bonus (体验金) and fee deduction (抵扣金)
-- vouchers. A voucher is activated by the user; activation grants the
-- matching bonus in the futures service (ref coupon:<id>).
CREATE TABLE g_coupons (
    id           BIGSERIAL PRIMARY KEY,
    uid          TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('trial','deduction')),
    amount       NUMERIC(36,8) NOT NULL CHECK (amount > 0),
    title        TEXT NOT NULL,
    source       TEXT NOT NULL UNIQUE,          -- idempotency key of the issuing program
    status       TEXT NOT NULL CHECK (status IN ('issued','active','expired','revoked')),
    activate_by  TIMESTAMPTZ NOT NULL,
    valid_days   INT NOT NULL,
    activated_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX g_coupons_uid ON g_coupons (uid, status);

-- Points carnival periods (积分嘉年华). config holds the earning rules and
-- the redeem table of that period (each period may differ).
CREATE TABLE g_campaigns (
    id         BIGSERIAL PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL DEFAULT 'points',
    title      TEXT NOT NULL,
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL CHECK (ends_at > starts_at),
    config     JSONB NOT NULL,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE g_enrollments (
    campaign_id BIGINT NOT NULL REFERENCES g_campaigns(id),
    uid         TEXT NOT NULL,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (campaign_id, uid)
);

CREATE TABLE g_points_daily (
    campaign_id BIGINT NOT NULL REFERENCES g_campaigns(id),
    uid         TEXT NOT NULL,
    day         DATE NOT NULL,                   -- UTC day (tasks reset at 00:00 UTC)
    task        TEXT NOT NULL CHECK (task IN ('volume','deposit','invite')),
    progress    NUMERIC(36,8) NOT NULL DEFAULT 0,
    points      INT NOT NULL,
    PRIMARY KEY (campaign_id, uid, day, task)
);

CREATE TABLE g_redemptions (
    id          BIGSERIAL PRIMARY KEY,
    campaign_id BIGINT NOT NULL REFERENCES g_campaigns(id),
    uid         TEXT NOT NULL,
    points      INT NOT NULL,                    -- the tier (its cost)
    coupon_id   BIGINT NOT NULL REFERENCES g_coupons(id),
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (campaign_id, uid, points)            -- each tier once per user
);

-- Time a user became a "valid" invitee (cumulative fees >= threshold).
ALTER TABLE g_users ADD COLUMN valid_at TIMESTAMPTZ;

-- Period 5 (ended). Physical prizes are not offered (USDT vouchers only).
INSERT INTO g_campaigns (key, title, starts_at, ends_at, config, created_by) VALUES
('carnival-5', '积分嘉年华 · 第五期', '2026-09-23 00:00:00+00', '2026-10-22 23:59:59+00',
 '{"volumeTiers":[{"volume":"2000","points":1},{"volume":"10000","points":1},{"volume":"100000","points":3},{"volume":"200000","points":3},{"volume":"300000","points":5},{"volume":"500000","points":8},{"volume":"1000000","points":12},{"volume":"3000000","points":35},{"volume":"10000000","points":100}],
   "depositUnit":"100","depositCap":10,"invitePoints":8,"inviteCap":80,
   "redeem":[{"points":10,"kind":"deduction","amount":"3"},{"points":20,"kind":"deduction","amount":"6"},{"points":35,"kind":"trial","amount":"10"},{"points":50,"kind":"deduction","amount":"10"},{"points":75,"kind":"trial","amount":"20"},{"points":100,"kind":"trial","amount":"30"},{"points":200,"kind":"deduction","amount":"75"},{"points":300,"kind":"trial","amount":"100"}]}',
 'seed');

-- +goose Down
ALTER TABLE g_users DROP COLUMN valid_at;
DROP TABLE g_redemptions;
DROP TABLE g_points_daily;
DROP TABLE g_enrollments;
DROP TABLE g_campaigns;
DROP TABLE g_coupons;
