-- +goose Up
-- Trader applications are reviewed manually in the admin console.
ALTER TABLE fut_copy_traders DROP CONSTRAINT IF EXISTS fut_copy_traders_status_check;
ALTER TABLE fut_copy_traders ADD CONSTRAINT fut_copy_traders_status_check CHECK (status IN ('pending','active','closed','rejected'));
ALTER TABLE fut_copy_traders ADD COLUMN review_note TEXT NOT NULL DEFAULT '';
ALTER TABLE fut_copy_traders ADD COLUMN reviewed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE fut_copy_traders ADD COLUMN reviewed_at TIMESTAMPTZ;

-- +goose Down
UPDATE fut_copy_traders SET status='closed' WHERE status='rejected';
ALTER TABLE fut_copy_traders DROP COLUMN reviewed_at;
ALTER TABLE fut_copy_traders DROP COLUMN reviewed_by;
ALTER TABLE fut_copy_traders DROP COLUMN review_note;
ALTER TABLE fut_copy_traders DROP CONSTRAINT fut_copy_traders_status_check;
ALTER TABLE fut_copy_traders ADD CONSTRAINT fut_copy_traders_status_check CHECK (status IN ('pending','active','closed'));
