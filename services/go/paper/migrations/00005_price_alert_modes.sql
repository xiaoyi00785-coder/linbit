-- +goose Up
ALTER TABLE price_alerts DROP CONSTRAINT price_alerts_trigger_mode_check;
ALTER TABLE price_alerts ADD CONSTRAINT price_alerts_trigger_mode_check CHECK (trigger_mode IN ('once', 'daily', 'repeat'));

-- +goose Down
UPDATE price_alerts SET trigger_mode = 'once' WHERE trigger_mode <> 'once';
ALTER TABLE price_alerts DROP CONSTRAINT price_alerts_trigger_mode_check;
ALTER TABLE price_alerts ADD CONSTRAINT price_alerts_trigger_mode_check CHECK (trigger_mode = 'once');
