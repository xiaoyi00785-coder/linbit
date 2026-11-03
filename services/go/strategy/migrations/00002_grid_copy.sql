-- +goose Up
ALTER TABLE grid_bots ADD COLUMN copied_from BIGINT REFERENCES grid_bots(id);
ALTER TABLE grid_bots ADD COLUMN copy_count INT NOT NULL DEFAULT 0;
CREATE INDEX grid_bots_copied_from ON grid_bots (copied_from) WHERE copied_from IS NOT NULL;

-- +goose Down
DROP INDEX grid_bots_copied_from;
ALTER TABLE grid_bots DROP COLUMN copy_count;
ALTER TABLE grid_bots DROP COLUMN copied_from;
