-- +goose Up
-- Newcomer gift package (新人礼包): one enrollment per user (KYC required),
-- each task claimed once; rewards are vouchers in g_coupons.
CREATE TABLE g_newcomer (
    uid         TEXT PRIMARY KEY,
    enrolled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deadline    TIMESTAMPTZ NOT NULL
);
CREATE TABLE g_newcomer_claims (
    uid       TEXT NOT NULL,
    task      TEXT NOT NULL,
    coupon_id BIGINT NOT NULL REFERENCES g_coupons(id),
    at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (uid, task)
);
-- Invite reward on the invitee's KYC (both sides get a trial voucher).
ALTER TABLE g_users ADD COLUMN invite_reward_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE g_users DROP COLUMN invite_reward_at;
DROP TABLE g_newcomer_claims;
DROP TABLE g_newcomer;
