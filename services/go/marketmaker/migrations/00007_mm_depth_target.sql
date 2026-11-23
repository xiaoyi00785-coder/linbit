-- +goose Up
-- Depth mode for large futures market orders (20–50 BTC). 0 = off (old
-- behaviour). depth_target_qty is base units per side that must rest within
-- depth_band_bps of mid; the maker thickens far levels when the copied venue
-- book is thinner than that.
ALTER TABLE mm_pairs
	ADD COLUMN depth_target_qty NUMERIC(36,18) NOT NULL DEFAULT 0,
	ADD COLUMN depth_band_bps   NUMERIC(10,4)  NOT NULL DEFAULT 30;

-- +goose Down
ALTER TABLE mm_pairs DROP COLUMN depth_target_qty, DROP COLUMN depth_band_bps;
