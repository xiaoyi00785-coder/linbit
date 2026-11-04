-- +goose Up

-- Admin-editable settings (key/value) for thresholds that must change
-- without a redeploy. Env vars are the defaults.
CREATE TABLE IF NOT EXISTS admin_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Manual balance adjustments (人工调账). Signed amount: >0 credit, <0 debit.
--   below threshold: applying -> applied | failed
--   at/above:        pending_approval -> applying -> applied | failed
--                    pending_approval -> rejected
-- The ledger write itself lives in asset (tx_type admin_adjust, idempotent on ref).
CREATE TABLE balance_adjustments (
    id           BIGSERIAL PRIMARY KEY,
    ref          TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL,
    asset        TEXT NOT NULL,
    account      TEXT NOT NULL CHECK (account IN ('spot', 'funding')),
    amount       NUMERIC(36,8) NOT NULL CHECK (amount <> 0),
    usdt_value   NUMERIC(36,8),
    needs_second BOOLEAN NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('pending_approval', 'applying', 'applied', 'rejected', 'failed')),
    reason       TEXT NOT NULL CHECK (reason <> ''),
    created_by   BIGINT NOT NULL REFERENCES admin_users(id),
    created_ip   TEXT,
    reviewed_by  BIGINT REFERENCES admin_users(id),
    review_note  TEXT,
    reviewed_at  TIMESTAMPTZ,
    reviewed_ip  TEXT,
    free_before  NUMERIC(36,8),
    free_after   NUMERIC(36,8),
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at   TIMESTAMPTZ,
    -- maker-checker: the creator can never be the reviewer
    CONSTRAINT balance_adjustments_four_eyes CHECK (reviewed_by IS NULL OR reviewed_by <> created_by)
);
CREATE INDEX idx_balance_adjustments_status ON balance_adjustments (status, created_at DESC);
CREATE INDEX idx_balance_adjustments_user ON balance_adjustments (user_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS balance_adjustments;
DROP TABLE IF EXISTS admin_settings;
