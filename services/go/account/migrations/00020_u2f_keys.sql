-- +goose Up
-- U2F / external security keys (YubiKey, Titan, etc.). Separate from the
-- passkeys table so the two can be listed and managed independently: passkeys
-- are platform authenticators (Face ID / Touch ID / Windows Hello) while
-- u2f_credentials holds cross-platform (USB / NFC) authenticators. We reuse
-- the WebAuthn library for both; the attachment hint and this table are what
-- separate them at the UI level.
CREATE TABLE IF NOT EXISTS u2f_credentials (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,         -- base64url credential id
    name          TEXT NOT NULL DEFAULT '',
    credential    JSONB NOT NULL,               -- webauthn.Credential (public key only)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_u2f_credentials_user ON u2f_credentials (user_id);

-- Ceremony sessions reuse the webauthn_sessions table (kind = 'u2f_register' |
-- 'u2f_auth'), no new storage required.

-- +goose Down
DROP TABLE IF EXISTS u2f_credentials;
