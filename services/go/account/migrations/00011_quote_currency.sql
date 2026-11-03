-- +goose Up
-- Per-user display (quote) currency preference: the single source of truth
-- for the fiat conversion currency shown by every client surface.
-- NULL = use the service default (ACCOUNT_DEFAULT_QUOTE_CURRENCY).
ALTER TABLE users ADD COLUMN IF NOT EXISTS quote_currency TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS quote_currency;
