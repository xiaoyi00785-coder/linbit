-- +goose Up
-- Network picker hints; NULL = keep the registry default.
ALTER TABLE wallet_network_settings
    ADD COLUMN recommended BOOLEAN,
    ADD COLUMN sort_order  INT;

-- +goose Down
ALTER TABLE wallet_network_settings
    DROP COLUMN IF EXISTS sort_order,
    DROP COLUMN IF EXISTS recommended;
