-- +goose Up
-- Follow-level symbol whitelist and USDT stop-loss; trader privacy / invite / logo.
ALTER TABLE fut_copy_follows
    ADD COLUMN support_symbols TEXT NOT NULL DEFAULT '',
    ADD COLUMN stop_loss_amount NUMERIC(36,8) NOT NULL DEFAULT 0 CHECK (stop_loss_amount >= 0);

ALTER TABLE fut_copy_traders
    ADD COLUMN logo_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN invitation_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN position_privacy BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE fut_copy_traders DROP COLUMN IF EXISTS position_privacy;
ALTER TABLE fut_copy_traders DROP COLUMN IF EXISTS invitation_code;
ALTER TABLE fut_copy_traders DROP COLUMN IF EXISTS logo_url;
ALTER TABLE fut_copy_follows DROP COLUMN IF EXISTS stop_loss_amount;
ALTER TABLE fut_copy_follows DROP COLUMN IF EXISTS support_symbols;
