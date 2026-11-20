-- +goose Up
-- Account self-deletion with a 7-day cool-off window, and Telegram account binding.
--
-- A delete request is pending from scheduled_at - 7 days up to scheduled_at;
-- the user may cancel any time while status = 'pending'. The sweeper then
-- flips the user to status = 'deleted' and scrubs PII, keeping only what
-- regulators require (order history is kept elsewhere for 7 years).
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'frozen', 'deleted'));

CREATE TABLE account_deletion_requests (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'cancelled', 'completed')),
    requested_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    scheduled_at  TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ,
    cancelled_at  TIMESTAMPTZ,
    reason        TEXT NOT NULL DEFAULT ''
);
-- Only one pending request per user at a time.
CREATE UNIQUE INDEX uniq_account_deletion_pending
    ON account_deletion_requests (user_id)
    WHERE status = 'pending';
CREATE INDEX idx_account_deletion_due
    ON account_deletion_requests (scheduled_at)
    WHERE status = 'pending';

-- Telegram binding (optional; one TG id per user, and a TG id can bind only one user).
ALTER TABLE users ADD COLUMN telegram_id        BIGINT;
ALTER TABLE users ADD COLUMN telegram_username  TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN telegram_bound_at  TIMESTAMPTZ;
CREATE UNIQUE INDEX uniq_users_telegram_id
    ON users (telegram_id)
    WHERE telegram_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS uniq_users_telegram_id;
ALTER TABLE users DROP COLUMN telegram_bound_at;
ALTER TABLE users DROP COLUMN telegram_username;
ALTER TABLE users DROP COLUMN telegram_id;
DROP INDEX IF EXISTS idx_account_deletion_due;
DROP INDEX IF EXISTS uniq_account_deletion_pending;
DROP TABLE IF EXISTS account_deletion_requests;
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'frozen'));
