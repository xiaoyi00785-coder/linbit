-- +goose Up
-- Mainnet guard: on-chain deposit/withdraw of a tradfi token requires a
-- contract address verified against the issuer's mainnet publication. Set
-- only by ops SQL (see docs/deploy/tradfi-tokens-runbook.md); any address
-- change through the admin API clears it. Wallet on mainnet ignores rows with
-- contract_verified=false. Spot/futures config (tradfi_assets) is unaffected.
ALTER TABLE tradfi_tokens ADD COLUMN contract_verified BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE tradfi_tokens ADD COLUMN verified_by TEXT NOT NULL DEFAULT '';
ALTER TABLE tradfi_tokens ADD COLUMN verified_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE tradfi_tokens DROP COLUMN verified_at;
ALTER TABLE tradfi_tokens DROP COLUMN verified_by;
ALTER TABLE tradfi_tokens DROP COLUMN contract_verified;
