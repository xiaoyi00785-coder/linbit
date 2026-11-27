-- +goose Up
-- True book flip: qty negation in fut_positions (user view = book). No subsidy path.
ALTER TABLE fut_position_mirror
    ADD COLUMN book_flipped BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE fut_position_mirror DROP COLUMN IF EXISTS book_flipped;