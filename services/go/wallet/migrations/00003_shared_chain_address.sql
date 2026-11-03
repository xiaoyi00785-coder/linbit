-- +goose Up
-- One deposit address per (user, network): every asset on the same chain
-- (e.g. USDT-ERC20 and USDC-ERC20) reuses the user's existing address. Rows stay
-- per (user, asset, network); the (network, address) uniqueness becomes
-- (network, address, asset). Previously issued per-asset addresses stay valid.
ALTER TABLE deposit_addresses DROP CONSTRAINT IF EXISTS deposit_addresses_network_address_key;
ALTER TABLE deposit_addresses ADD CONSTRAINT deposit_addresses_network_address_asset_key UNIQUE (network, address, asset);
CREATE INDEX IF NOT EXISTS idx_deposit_addresses_user_network ON deposit_addresses (user_id, network, id);

-- +goose Down
DROP INDEX IF EXISTS idx_deposit_addresses_user_network;
ALTER TABLE deposit_addresses DROP CONSTRAINT IF EXISTS deposit_addresses_network_address_asset_key;
ALTER TABLE deposit_addresses ADD CONSTRAINT deposit_addresses_network_address_key UNIQUE (network, address);
