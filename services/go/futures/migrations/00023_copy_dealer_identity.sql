-- +goose Up
-- Restores the copytrade columns whose migrations (18-20) were lost from the
-- source tree. The live DB already has these columns (applied as 18-20), so the
-- IF NOT EXISTS guards make this a no-op there; fresh databases and the test
-- suite get the columns from here.
ALTER TABLE fut_copy_traders ADD COLUMN IF NOT EXISTS dealer_identity text NOT NULL DEFAULT 'PUBLIC_AREA';
ALTER TABLE fut_copy_traders ADD COLUMN IF NOT EXISTS closed_at timestamptz;
ALTER TABLE fut_copy_follows ADD COLUMN IF NOT EXISTS leverage integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE fut_copy_follows DROP COLUMN IF EXISTS leverage;
ALTER TABLE fut_copy_traders DROP COLUMN IF EXISTS closed_at;
ALTER TABLE fut_copy_traders DROP COLUMN IF EXISTS dealer_identity;
