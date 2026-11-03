-- +goose Up
-- Growth service: invite attribution, partner (agent) commissions and the
-- regular referral rebate. Data is pulled from the other services through
-- cursor feeds (g_cursors) and settled daily into the funding account.

CREATE TABLE g_cursors (
    name TEXT PRIMARY KEY,
    pos  BIGINT NOT NULL DEFAULT 0
);

-- One row per platform user; inviter is fixed at registration (never changes).
CREATE TABLE g_users (
    uid         TEXT PRIMARY KEY,
    invite_code TEXT NOT NULL UNIQUE,          -- this user's own code
    inviter     TEXT,                          -- uid of the inviter (one level)
    kyc_level   INT NOT NULL DEFAULT 0,
    registered_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX g_users_inviter ON g_users (inviter);

CREATE TABLE g_partners (
    uid          TEXT PRIMARY KEY,
    status       TEXT NOT NULL CHECK (status IN ('pending','active','rejected','disabled')),
    tier         TEXT NOT NULL DEFAULT 'normal',
    contact      TEXT NOT NULL,
    channel      TEXT NOT NULL DEFAULT '',
    scale        TEXT NOT NULL DEFAULT '',
    review_note  TEXT NOT NULL DEFAULT '',
    reviewed_by  TEXT NOT NULL DEFAULT '',
    volume30d    NUMERIC(36,8) NOT NULL DEFAULT 0,
    applied_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at  TIMESTAMPTZ
);

-- Every fee a user paid (spot or futures), converted to USDT. base = fee -
-- bonus_fee - coupon_fee is the rebate base (trial-bonus and fee-coupon
-- covered fees never earn rebates).
CREATE TABLE g_fee_events (
    id            BIGSERIAL PRIMARY KEY,
    src           TEXT NOT NULL,               -- spot | futures
    src_id        TEXT NOT NULL,
    uid           TEXT NOT NULL,
    product       TEXT NOT NULL CHECK (product IN ('spot','futures')),
    notional      NUMERIC(36,8) NOT NULL,      -- USDT
    fee           NUMERIC(36,12) NOT NULL,     -- USDT
    bonus_fee     NUMERIC(36,12) NOT NULL DEFAULT 0,
    coupon_fee    NUMERIC(36,12) NOT NULL DEFAULT 0,
    copy          BOOLEAN NOT NULL DEFAULT FALSE,
    at            TIMESTAMPTZ NOT NULL,
    settlement_id BIGINT,
    UNIQUE (src, src_id)
);
CREATE INDEX g_fee_events_open ON g_fee_events (at) WHERE settlement_id IS NULL;
CREATE INDEX g_fee_events_uid ON g_fee_events (uid, at);

CREATE TABLE g_wallet_ops (
    seq    BIGINT PRIMARY KEY,
    ref    TEXT NOT NULL,
    op     TEXT NOT NULL,
    uid    TEXT NOT NULL,
    asset  TEXT NOT NULL,
    amount NUMERIC(36,18) NOT NULL,
    at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX g_wallet_ops_uid ON g_wallet_ops (uid, op, at);

CREATE TABLE g_settlements (
    id         BIGSERIAL PRIMARY KEY,
    cutoff     TIMESTAMPTZ NOT NULL UNIQUE,
    events     INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Aggregated per (settlement, beneficiary, source user, kind, UTC+8 day).
CREATE TABLE g_commissions (
    id            BIGSERIAL PRIMARY KEY,
    settlement_id BIGINT NOT NULL REFERENCES g_settlements(id),
    beneficiary   TEXT NOT NULL,
    source_uid    TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('futures','spot','override','referral')),
    tier          TEXT NOT NULL DEFAULT '',
    rate          NUMERIC(10,6) NOT NULL,
    base          NUMERIC(36,12) NOT NULL,
    amount        NUMERIC(36,12) NOT NULL,
    day           DATE NOT NULL,
    UNIQUE (settlement_id, beneficiary, source_uid, kind, day)
);
CREATE INDEX g_commissions_ben ON g_commissions (beneficiary, day);

CREATE TABLE g_payouts (
    settlement_id BIGINT NOT NULL REFERENCES g_settlements(id),
    uid           TEXT NOT NULL,
    amount        NUMERIC(36,8) NOT NULL,
    ref           TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL CHECK (status IN ('pending','paid')),
    paid_at       TIMESTAMPTZ,
    PRIMARY KEY (settlement_id, uid)
);

-- +goose Down
DROP TABLE g_payouts;
DROP TABLE g_commissions;
DROP TABLE g_settlements;
DROP TABLE g_wallet_ops;
DROP TABLE g_fee_events;
DROP TABLE g_partners;
DROP TABLE g_users;
DROP TABLE g_cursors;
