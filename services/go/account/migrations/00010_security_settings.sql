-- +goose Up
-- Account security settings: fund password, anti-phishing code, security
-- withdrawal lock, passkeys (WebAuthn) and admin-tunable security options.
ALTER TABLE users ADD COLUMN IF NOT EXISTS fund_password_hash   TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS fund_pw_failed       INT  NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS fund_pw_locked_until TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS anti_phishing_code   TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS withdraw_locked_until TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS withdraw_lock_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS account_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS passkeys (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,         -- base64url credential id
    name          TEXT NOT NULL DEFAULT '',
    credential    JSONB NOT NULL,               -- webauthn.Credential (public key only)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_passkeys_user ON passkeys (user_id);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
    id         BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    user_id    BIGINT,                          -- NULL for discoverable login
    kind       TEXT NOT NULL,                   -- register | login
    data       JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);


-- +goose Down
DROP TABLE IF EXISTS webauthn_sessions;
DROP TABLE IF EXISTS passkeys;
DROP TABLE IF EXISTS account_settings;
ALTER TABLE users DROP COLUMN IF EXISTS withdraw_lock_reason;
ALTER TABLE users DROP COLUMN IF EXISTS withdraw_locked_until;
ALTER TABLE users DROP COLUMN IF EXISTS anti_phishing_code;
ALTER TABLE users DROP COLUMN IF EXISTS fund_pw_locked_until;
ALTER TABLE users DROP COLUMN IF EXISTS fund_pw_failed;
ALTER TABLE users DROP COLUMN IF EXISTS fund_password_hash;
