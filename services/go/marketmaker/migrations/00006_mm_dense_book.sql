-- +goose Up
-- Dense book: 20 levels/side, 1 bp spacing, inventory cap high enough for
-- 100–1,000,000 USDT random quotes.
UPDATE mm_pairs SET
	levels = 20,
	level_step_bps = 1,
	max_position = GREATEST(max_position, 100000000);
ALTER TABLE mm_pairs ALTER COLUMN levels SET DEFAULT 20;
ALTER TABLE mm_pairs ALTER COLUMN level_step_bps SET DEFAULT 1;
ALTER TABLE mm_pairs ALTER COLUMN max_position SET DEFAULT 100000000;

-- +goose Down
ALTER TABLE mm_pairs ALTER COLUMN levels SET DEFAULT 10;
ALTER TABLE mm_pairs ALTER COLUMN level_step_bps SET DEFAULT 5;
ALTER TABLE mm_pairs ALTER COLUMN max_position SET DEFAULT 50000;
UPDATE mm_pairs SET levels = 10, level_step_bps = 5 WHERE levels = 20;
