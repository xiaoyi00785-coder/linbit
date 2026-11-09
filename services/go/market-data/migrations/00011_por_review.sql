-- +goose Up
-- Draft / review for proof-of-reserves.
-- Existing rows were already the public latest snapshot, so they stay
-- status=published. The column default is then switched to draft: this
-- migration inserts nothing and does not publish a new snapshot.
ALTER TABLE por_snapshots ADD COLUMN status TEXT NOT NULL DEFAULT 'published'
    CHECK (status IN ('draft', 'pending_review', 'published', 'rejected'));
ALTER TABLE por_snapshots ALTER COLUMN status SET DEFAULT 'draft';
ALTER TABLE por_snapshots ADD COLUMN submitted_by TEXT NOT NULL DEFAULT '';
ALTER TABLE por_snapshots ADD COLUMN reviewed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE por_snapshots ADD COLUMN reviewed_at TIMESTAMPTZ;
ALTER TABLE por_snapshots ADD COLUMN review_note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE por_snapshots DROP COLUMN review_note;
ALTER TABLE por_snapshots DROP COLUMN reviewed_at;
ALTER TABLE por_snapshots DROP COLUMN reviewed_by;
ALTER TABLE por_snapshots DROP COLUMN submitted_by;
ALTER TABLE por_snapshots DROP COLUMN status;
