-- +goose Up
ALTER TABLE convert_config ADD COLUMN IF NOT EXISTS hedge_cancel_after_seconds INT NOT NULL DEFAULT 30;
CREATE INDEX IF NOT EXISTS convert_hedges_resting_idx ON convert_hedges (asset) WHERE status = 'resting';

-- +goose Down
DROP INDEX IF EXISTS convert_hedges_resting_idx;
ALTER TABLE convert_config DROP COLUMN IF EXISTS hedge_cancel_after_seconds;
