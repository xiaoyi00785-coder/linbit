-- +goose Up
-- Funding history rows imported from a public exchange (startup backfill) are
-- marked so they are distinguishable from rates settled by this engine.
ALTER TABLE fut_funding_history ADD COLUMN source TEXT NOT NULL DEFAULT 'settle';

-- +goose Down
ALTER TABLE fut_funding_history DROP COLUMN source;
