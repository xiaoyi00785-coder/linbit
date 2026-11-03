-- +goose Up
-- Two bonus kinds: trial (体验金: fees + funding) and deduction (抵扣金: fees
-- only). Deduction is consumed before trial, earliest expiry first.
ALTER TABLE fut_bonus ADD COLUMN kind TEXT NOT NULL DEFAULT 'trial' CHECK (kind IN ('trial','deduction'));
ALTER TABLE fut_bonus ADD COLUMN source TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE fut_bonus DROP COLUMN source;
ALTER TABLE fut_bonus DROP COLUMN kind;
