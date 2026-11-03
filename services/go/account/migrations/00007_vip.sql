-- +goose Up
-- VIP applications (manual review, 1-3 business days). VIP carries service
-- benefits only (account manager, custom plans); fees are uniform for everyone.
CREATE TABLE vip_applications (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    channel     TEXT NOT NULL,
    contact     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    reason      TEXT NOT NULL DEFAULT '',
    reviewer    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ
);
-- At most one open (pending) or approved application per user.
CREATE UNIQUE INDEX uq_vip_open ON vip_applications (user_id) WHERE status IN ('pending','approved');
CREATE INDEX vip_applications_status ON vip_applications (status, id DESC);

-- +goose Down
DROP TABLE vip_applications;
