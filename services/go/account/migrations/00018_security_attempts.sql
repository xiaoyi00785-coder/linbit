-- +goose Up
-- Brute-force and replay protection for the second factor (TOTP) and password
-- login.
--
--   totp_failed / totp_locked_until   per-user TOTP failure counter and lock
--   totp_last_step                    last accepted 30s time step; a code is
--                                     accepted at most once (replay guard)
--   login_failed / login_locked_until per-account password failure counter/lock
ALTER TABLE users ADD COLUMN totp_failed        INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_locked_until  TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN totp_last_step     BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN login_failed       INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN login_locked_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN login_locked_until;
ALTER TABLE users DROP COLUMN login_failed;
ALTER TABLE users DROP COLUMN totp_last_step;
ALTER TABLE users DROP COLUMN totp_locked_until;
ALTER TABLE users DROP COLUMN totp_failed;
