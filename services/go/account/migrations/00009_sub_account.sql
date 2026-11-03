-- +goose Up
-- Sub-account marker: a non-empty parent_uid means the user is a sub-account
-- of that uid. Nothing creates sub-accounts yet; internal transfers refuse them.
ALTER TABLE users ADD COLUMN IF NOT EXISTS parent_uid TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS parent_uid;
