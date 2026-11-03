-- +goose Up
-- Admin-controlled account status. 'frozen' blocks login, token refresh and
-- withdrawal verification; the reason is kept for support and audit.
ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'frozen'));
ALTER TABLE users ADD COLUMN status_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN status_changed_at TIMESTAMPTZ;
CREATE INDEX idx_users_status ON users (status);

-- +goose Down
DROP INDEX IF EXISTS idx_users_status;
ALTER TABLE users DROP COLUMN status_changed_at;
ALTER TABLE users DROP COLUMN status_reason;
ALTER TABLE users DROP COLUMN status;
