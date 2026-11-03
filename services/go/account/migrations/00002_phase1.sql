-- +goose Up
-- Phase-1 account features: phone registration, verification codes, two-step
-- login challenges, device management, and the withdrawal address book.

-- --- users: phone + profile + verification flags -------------------------
-- Email is no longer mandatory (phone-only registration is allowed), but a
-- user must always have at least one contact handle.
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users ADD COLUMN phone            TEXT UNIQUE;
ALTER TABLE users ADD COLUMN country_code      TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN avatar_url        TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN email_verified    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN phone_verified    BOOLEAN NOT NULL DEFAULT FALSE;
-- Per-user switch: when on, withdrawals may only go to whitelisted addresses.
ALTER TABLE users ADD COLUMN withdraw_whitelist_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD CONSTRAINT users_contact_present CHECK (email IS NOT NULL OR phone IS NOT NULL);

-- --- verification_codes: email/SMS one-time codes ------------------------
-- Codes are stored hashed (never in plaintext). target is the email address or
-- E.164 phone number; purpose segregates register/login/reset_password/... so a
-- code issued for one flow cannot be replayed in another.
CREATE TABLE verification_codes (
    id           BIGSERIAL PRIMARY KEY,
    target       TEXT NOT NULL,
    channel      TEXT NOT NULL,              -- email | sms
    purpose      TEXT NOT NULL,              -- register | login | reset_password | address | bind
    code_hash    TEXT NOT NULL,
    attempts     INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    consumed     BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_vcodes_lookup ON verification_codes (target, purpose, consumed, created_at DESC);

-- --- devices: one row per (user, device fingerprint) --------------------
CREATE TABLE devices (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_key  TEXT NOT NULL,              -- stable fingerprint (client id or UA hash)
    name        TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    first_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, device_key)
);

-- Tie each refresh token to the device it was issued to so removing a device
-- can revoke exactly that device's sessions.
ALTER TABLE refresh_tokens ADD COLUMN device_key TEXT NOT NULL DEFAULT '';

-- --- login_challenges: short-lived second-factor tickets -----------------
CREATE TABLE login_challenges (
    id          BIGSERIAL PRIMARY KEY,
    token_hash  TEXT NOT NULL,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method      TEXT NOT NULL,             -- totp | email | sms
    device_key  TEXT NOT NULL DEFAULT '',
    device_name TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    consumed    BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_login_challenges_hash ON login_challenges (token_hash);

-- --- withdraw_addresses: the user's address book / whitelist -------------
CREATE TABLE withdraw_addresses (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    asset        TEXT NOT NULL,
    network      TEXT NOT NULL,
    address      TEXT NOT NULL,
    memo         TEXT NOT NULL DEFAULT '',
    label        TEXT NOT NULL DEFAULT '',
    whitelisted  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, asset, network, address, memo)
);
CREATE INDEX idx_withdraw_addresses_user ON withdraw_addresses (user_id, created_at DESC);

-- +goose Down
DROP TABLE withdraw_addresses;
DROP TABLE login_challenges;
ALTER TABLE refresh_tokens DROP COLUMN device_key;
DROP TABLE devices;
DROP TABLE verification_codes;
ALTER TABLE users DROP CONSTRAINT users_contact_present;
ALTER TABLE users DROP COLUMN withdraw_whitelist_enabled;
ALTER TABLE users DROP COLUMN phone_verified;
ALTER TABLE users DROP COLUMN email_verified;
ALTER TABLE users DROP COLUMN avatar_url;
ALTER TABLE users DROP COLUMN country_code;
ALTER TABLE users DROP COLUMN phone;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
