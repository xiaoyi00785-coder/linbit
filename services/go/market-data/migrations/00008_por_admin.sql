-- +goose Up
-- Proof-of-reserves snapshots are entered manually by admin (no auto computation).
ALTER TABLE por_snapshots ADD COLUMN created_by TEXT NOT NULL DEFAULT '';
ALTER TABLE por_snapshots ADD COLUMN note TEXT NOT NULL DEFAULT '';
ALTER TABLE por_snapshots ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE por_snapshots DROP COLUMN created_at;
ALTER TABLE por_snapshots DROP COLUMN note;
ALTER TABLE por_snapshots DROP COLUMN created_by;
