-- +goose Up

-- Back-office operators. Accounts are disabled, never deleted.
CREATE TABLE admin_users (
    id              BIGSERIAL PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    totp_secret     TEXT,
    roles           TEXT[] NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    failed_attempts INT NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_by      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Opaque bearer sessions (sha256 of the token) with an idle timeout.
CREATE TABLE admin_sessions (
    token_hash   TEXT PRIMARY KEY,
    admin_id     BIGINT NOT NULL REFERENCES admin_users(id),
    ip           TEXT,
    user_agent   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked      BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX idx_admin_sessions_admin ON admin_sessions (admin_id);

-- Append-only audit trail: every write, sensitive read, export and login.
CREATE TABLE audit_logs (
    id         BIGSERIAL PRIMARY KEY,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    admin_id   BIGINT,
    admin_name TEXT,
    roles      TEXT[] NOT NULL DEFAULT '{}',
    module     TEXT NOT NULL,
    action     TEXT NOT NULL,
    target     TEXT,
    ip         TEXT,
    user_agent TEXT,
    request_id TEXT,
    level      TEXT NOT NULL CHECK (level IN ('normal', 'sensitive', 'high')),
    result     TEXT NOT NULL,
    before     JSONB,
    after      JSONB,
    reason     TEXT
);
CREATE INDEX idx_audit_logs_at ON audit_logs (at DESC);
CREATE INDEX idx_audit_logs_admin ON audit_logs (admin_id, at DESC);
CREATE INDEX idx_audit_logs_module ON audit_logs (module, at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_logs_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_audit_logs_immutable BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();

-- Change requests needing multi-party approval (currently: fee rates).
--   pending -> scheduled (all approvals in; effective_at set) -> applied
--   pending -> rejected | cancelled
CREATE TABLE change_requests (
    id                     BIGSERIAL PRIMARY KEY,
    kind                   TEXT NOT NULL,
    payload                JSONB NOT NULL,
    before                 JSONB,
    status                 TEXT NOT NULL DEFAULT 'pending',
    reason                 TEXT NOT NULL,
    submitted_by           BIGINT NOT NULL REFERENCES admin_users(id),
    requested_effective_at TIMESTAMPTZ,
    effective_at           TIMESTAMPTZ,
    announcement_id        BIGINT,
    rollback_of            BIGINT,
    applied_at             TIMESTAMPTZ,
    error                  TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_change_requests_status ON change_requests (status, effective_at);

CREATE TABLE change_approvals (
    change_id BIGINT NOT NULL REFERENCES change_requests(id),
    admin_id  BIGINT NOT NULL REFERENCES admin_users(id),
    as_role   TEXT NOT NULL,
    decision  TEXT NOT NULL CHECK (decision IN ('approve', 'reject')),
    comment   TEXT,
    at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (change_id, admin_id)
);

-- Public announcements (e.g. the 24h fee-change notice).
CREATE TABLE announcements (
    id         BIGSERIAL PRIMARY KEY,
    category   TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    publish_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Coupon templates; budget = face_value * quantity (USDT). Budgets above the
-- threshold wait for finance approval.
CREATE TABLE coupon_templates (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    face_value  NUMERIC(36,8) NOT NULL,
    quantity    INT NOT NULL,
    budget      NUMERIC(36,8) NOT NULL,
    min_spend   NUMERIC(36,8) NOT NULL DEFAULT 0,
    valid_days  INT NOT NULL,
    per_user    INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL,             -- pending_finance | active | rejected
    created_by  BIGINT NOT NULL REFERENCES admin_users(id),
    reviewed_by BIGINT REFERENCES admin_users(id),
    review_note TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Manual reward reissues; above the threshold they wait for finance review.
CREATE TABLE reward_grants (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    asset       TEXT NOT NULL,
    amount      NUMERIC(36,8) NOT NULL,
    usdt_value  NUMERIC(36,8),
    reason      TEXT NOT NULL,
    status      TEXT NOT NULL,             -- pending_finance | executed | rejected | failed
    created_by  BIGINT NOT NULL REFERENCES admin_users(id),
    reviewed_by BIGINT REFERENCES admin_users(id),
    review_note TEXT,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    executed_at TIMESTAMPTZ
);

-- Two-person withdrawal review state (the withdrawal itself lives in wallet).
CREATE TABLE withdrawal_reviews (
    withdrawal_id TEXT PRIMARY KEY,
    needs_second  BOOLEAN NOT NULL,
    first_by      BIGINT REFERENCES admin_users(id),
    first_at      TIMESTAMPTZ,
    first_comment TEXT,
    second_by     BIGINT REFERENCES admin_users(id),
    second_at     TIMESTAMPTZ,
    second_comment TEXT,
    status        TEXT NOT NULL,           -- awaiting_second | approved | rejected
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE withdrawal_reviews;
DROP TABLE reward_grants;
DROP TABLE coupon_templates;
DROP TABLE announcements;
DROP TABLE change_approvals;
DROP TABLE change_requests;
DROP TRIGGER trg_audit_logs_immutable ON audit_logs;
DROP FUNCTION audit_logs_immutable();
DROP TABLE audit_logs;
DROP TABLE admin_sessions;
DROP TABLE admin_users;
