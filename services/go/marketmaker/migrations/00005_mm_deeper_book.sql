-- +goose Up
-- 5 levels/side is a thin book (10 quotes). 10/side fills the ladder without
-- hitting the admin cap (20).
UPDATE mm_pairs SET levels = 10 WHERE levels < 10;
ALTER TABLE mm_pairs ALTER COLUMN levels SET DEFAULT 10;

-- +goose Down
ALTER TABLE mm_pairs ALTER COLUMN levels SET DEFAULT 5;
UPDATE mm_pairs SET levels = 5 WHERE levels = 10;
