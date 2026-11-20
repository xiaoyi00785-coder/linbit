-- +goose Up
-- Advanced order round 2:
--   * trailing stop (new algo kind) with activation price + callback rate/amount
--     and a peak tracker;
--   * INDEX as a third trigger-price source (was LAST | MARK);
--   * auto-margin settings per (user, symbol): on a low margin ratio the engine
--     tops up the isolated position from the futures wallet up to a cap.
-- Nothing in this migration drops data: constraints are widened, not narrowed.

-- Widen algo-order kind to include trailing_stop, and the trigger reference to INDEX.
ALTER TABLE fut_algo_orders DROP CONSTRAINT fut_algo_orders_kind_check;
ALTER TABLE fut_algo_orders ADD CONSTRAINT fut_algo_orders_kind_check
    CHECK (kind IN ('tp','sl','plan','trailing_stop'));
ALTER TABLE fut_algo_orders DROP CONSTRAINT fut_algo_orders_trigger_type_check;
ALTER TABLE fut_algo_orders ADD CONSTRAINT fut_algo_orders_trigger_type_check
    CHECK (trigger_type IN ('LAST','MARK','INDEX'));

-- Trailing-stop state (NULL on tp/sl/plan rows).
ALTER TABLE fut_algo_orders
    ADD COLUMN activation_price NUMERIC(36,18),
    ADD COLUMN callback_rate    NUMERIC(10,6),  -- 0.001..0.10 (0.1%..10%)
    ADD COLUMN callback_amount  NUMERIC(36,18), -- absolute price delta (alternative)
    ADD COLUMN peak_price       NUMERIC(36,18),
    ADD COLUMN activated_at     TIMESTAMPTZ;

-- Widen tp/sl trigger types on the main order and position tables to include INDEX.
ALTER TABLE fut_orders DROP CONSTRAINT fut_orders_tp_trigger_type_check;
ALTER TABLE fut_orders ADD CONSTRAINT fut_orders_tp_trigger_type_check
    CHECK (tp_trigger_type IN ('LAST','MARK','INDEX'));
ALTER TABLE fut_orders DROP CONSTRAINT fut_orders_sl_trigger_type_check;
ALTER TABLE fut_orders ADD CONSTRAINT fut_orders_sl_trigger_type_check
    CHECK (sl_trigger_type IN ('LAST','MARK','INDEX'));
ALTER TABLE fut_positions DROP CONSTRAINT fut_positions_tp_trigger_type_check;
ALTER TABLE fut_positions ADD CONSTRAINT fut_positions_tp_trigger_type_check
    CHECK (tp_trigger_type IN ('LAST','MARK','INDEX'));
ALTER TABLE fut_positions DROP CONSTRAINT fut_positions_sl_trigger_type_check;
ALTER TABLE fut_positions ADD CONSTRAINT fut_positions_sl_trigger_type_check
    CHECK (sl_trigger_type IN ('LAST','MARK','INDEX'));

-- Per (user, symbol) auto-margin switch. check_ratio is the maintenance-margin
-- ratio (position_mm / (iso_margin + upnl)) at which a top-up fires; top_up_to
-- is the margin ratio we target after top-up (0.30 = 30%). max_amount caps the
-- total that auto-margin may add over the life of the position.
ALTER TABLE fut_settings
    ADD COLUMN auto_margin_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN auto_margin_max     NUMERIC(36,18) NOT NULL DEFAULT 0,
    ADD COLUMN auto_margin_trigger NUMERIC(10,6)  NOT NULL DEFAULT 0.15,
    ADD COLUMN auto_margin_used    NUMERIC(36,18) NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE fut_settings
    DROP COLUMN auto_margin_used,
    DROP COLUMN auto_margin_trigger,
    DROP COLUMN auto_margin_max,
    DROP COLUMN auto_margin_enabled;
ALTER TABLE fut_positions DROP CONSTRAINT fut_positions_sl_trigger_type_check;
ALTER TABLE fut_positions ADD CONSTRAINT fut_positions_sl_trigger_type_check
    CHECK (sl_trigger_type IN ('LAST','MARK'));
ALTER TABLE fut_positions DROP CONSTRAINT fut_positions_tp_trigger_type_check;
ALTER TABLE fut_positions ADD CONSTRAINT fut_positions_tp_trigger_type_check
    CHECK (tp_trigger_type IN ('LAST','MARK'));
ALTER TABLE fut_orders DROP CONSTRAINT fut_orders_sl_trigger_type_check;
ALTER TABLE fut_orders ADD CONSTRAINT fut_orders_sl_trigger_type_check
    CHECK (sl_trigger_type IN ('LAST','MARK'));
ALTER TABLE fut_orders DROP CONSTRAINT fut_orders_tp_trigger_type_check;
ALTER TABLE fut_orders ADD CONSTRAINT fut_orders_tp_trigger_type_check
    CHECK (tp_trigger_type IN ('LAST','MARK'));
ALTER TABLE fut_algo_orders
    DROP COLUMN activated_at,
    DROP COLUMN peak_price,
    DROP COLUMN callback_amount,
    DROP COLUMN callback_rate,
    DROP COLUMN activation_price;
ALTER TABLE fut_algo_orders DROP CONSTRAINT fut_algo_orders_trigger_type_check;
ALTER TABLE fut_algo_orders ADD CONSTRAINT fut_algo_orders_trigger_type_check
    CHECK (trigger_type IN ('LAST','MARK'));
ALTER TABLE fut_algo_orders DROP CONSTRAINT fut_algo_orders_kind_check;
ALTER TABLE fut_algo_orders ADD CONSTRAINT fut_algo_orders_kind_check
    CHECK (kind IN ('tp','sl','plan'));
