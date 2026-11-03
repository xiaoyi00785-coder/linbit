-- +goose Up
-- General transfer (POST /assets/transfer): client-supplied idempotency key
-- and the asset column for the unified history. Existing rows (POST
-- /futures/transfer, copy profit share) keep client_id NULL and asset USDT.
ALTER TABLE fut_transfers ADD COLUMN client_id TEXT;
ALTER TABLE fut_transfers ADD COLUMN asset TEXT NOT NULL DEFAULT 'USDT';
CREATE UNIQUE INDEX fut_transfers_client_id ON fut_transfers (user_id, client_id) WHERE client_id IS NOT NULL;
CREATE INDEX fut_transfers_user_created ON fut_transfers (user_id, created_at DESC);

-- +goose Down
DROP INDEX fut_transfers_user_created;
DROP INDEX fut_transfers_client_id;
ALTER TABLE fut_transfers DROP COLUMN asset;
ALTER TABLE fut_transfers DROP COLUMN client_id;
