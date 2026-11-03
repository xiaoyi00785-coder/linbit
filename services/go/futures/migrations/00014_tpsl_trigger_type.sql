-- +goose Up
-- Trigger price type (LAST/MARK) for TP/SL attached at order placement and stored
-- on the position. Existing rows keep MARK (previous behaviour).
-- Numbered 00014 because 00013 (funding source) is on fix/acceptance-r2.
ALTER TABLE fut_orders
    ADD COLUMN tp_trigger_type TEXT NOT NULL DEFAULT 'MARK' CHECK (tp_trigger_type IN ('LAST','MARK')),
    ADD COLUMN sl_trigger_type TEXT NOT NULL DEFAULT 'MARK' CHECK (sl_trigger_type IN ('LAST','MARK'));
ALTER TABLE fut_positions
    ADD COLUMN tp_trigger_type TEXT NOT NULL DEFAULT 'MARK' CHECK (tp_trigger_type IN ('LAST','MARK')),
    ADD COLUMN sl_trigger_type TEXT NOT NULL DEFAULT 'MARK' CHECK (sl_trigger_type IN ('LAST','MARK'));

-- +goose Down
ALTER TABLE fut_positions DROP COLUMN sl_trigger_type, DROP COLUMN tp_trigger_type;
ALTER TABLE fut_orders DROP COLUMN sl_trigger_type, DROP COLUMN tp_trigger_type;
