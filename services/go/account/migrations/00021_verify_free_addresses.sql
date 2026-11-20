-- +goose Up
-- Withdrawal-address verify-free mode. When a user has confirmed an address
-- with their second factor, they can mark it verify-free for 30 days or
-- permanently; subsequent withdrawals to that exact (asset, network, address)
-- then bypass the TOTP / email / SMS code check in the wallet verify flow.
--
--   verify_free_mode   'none' | '30_days' | 'permanent'
--   verify_free_until  when set, 30-day expiry (NULL for permanent / none)
ALTER TABLE withdraw_addresses
    ADD COLUMN IF NOT EXISTS verify_free_mode  TEXT        NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS verify_free_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_withdraw_addresses_verify_free_until
    ON withdraw_addresses (verify_free_until) WHERE verify_free_until IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_withdraw_addresses_verify_free_until;
ALTER TABLE withdraw_addresses
    DROP COLUMN IF EXISTS verify_free_until,
    DROP COLUMN IF EXISTS verify_free_mode;
