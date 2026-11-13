-- +goose Up
-- H1-H3: spot settlement from the matching engine's ordered result stream.
--
-- Each symbol has a stream cursor (`engine_cursors`) tracking the last
-- consumed entry. The consumer thread fetches new entries, settles trades
-- atomically (asset service idempotency on trade_id), and advances the cursor.
--
-- New orders land in state `new` with `engine_seen=false` and a short deadline
-- (engine time). The engine rejects expired places before the WAL write, so a
-- place that never appears in the stream and whose deadline is below the
-- stream's watermark is known lost. Orders remain `new` until their first
-- stream entry (place or cancel report) arrives; settlement drives every status
-- transition thereafter.

-- Per-symbol cursor over the engine's result stream. The consumer thread holds
-- an advisory lock on the symbol row and polls `GET /engine/reports?after=`.
CREATE TABLE engine_cursors (
    symbol     TEXT PRIMARY KEY,
    last_seq   BIGINT NOT NULL DEFAULT 0, -- last consumed entry
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- New orders columns for stream-based settlement.
ALTER TABLE orders ADD COLUMN engine_deadline BIGINT; -- unix ms; nil when no deadline was set
ALTER TABLE orders ADD COLUMN engine_seen BOOLEAN NOT NULL DEFAULT false; -- first report received
ALTER TABLE orders ADD COLUMN release_state TEXT NOT NULL DEFAULT 'none'; -- none | pending | done
ALTER TABLE orders ADD COLUMN review_reason TEXT; -- why manual review is needed

-- Partial index: orders awaiting their first report (sweep target).
CREATE INDEX idx_orders_pending_stream ON orders (symbol, engine_deadline) WHERE NOT engine_seen;

-- Partial index: orders with leftover release pending (sweep target).
CREATE INDEX idx_orders_release_pending ON orders (symbol) WHERE release_state = 'pending';

-- Fee rates frozen at order placement. One row per (order_id, role). Fees are
-- applied at settlement time using these rates, so a mid-flight rate change
-- does not affect resting orders. The row is written when the order reaches
-- the engine (engine_seen=true) and never updated; missing rows (legacy orders
-- from before this migration) use the configured default rate.
CREATE TABLE trade_fees (
    order_id TEXT NOT NULL,
    role     TEXT NOT NULL, -- maker | taker
    rate     NUMERIC(36,18) NOT NULL,
    PRIMARY KEY (order_id, role)
);

-- Settlement failures (dead letters). When a trade's asset settlement fails
-- after the engine applied it, the trade is logged here for manual review and
-- retry. The `reason` column holds the asset service error; `resolved_at` and
-- `resolved_by` track when it was fixed.
CREATE TABLE settlement_failures (
    id          BIGSERIAL PRIMARY KEY,
    trade_id    TEXT NOT NULL,
    order_id    TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    symbol      TEXT NOT NULL,
    reason      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT
);
CREATE INDEX idx_settlement_failures_unresolved ON settlement_failures (created_at) WHERE resolved_at IS NULL;
CREATE INDEX idx_settlement_failures_order ON settlement_failures (order_id);

-- +goose Down
DROP TABLE settlement_failures;
DROP TABLE trade_fees;
DROP INDEX idx_orders_release_pending;
DROP INDEX idx_orders_pending_stream;
ALTER TABLE orders DROP COLUMN review_reason;
ALTER TABLE orders DROP COLUMN release_state;
ALTER TABLE orders DROP COLUMN engine_seen;
ALTER TABLE orders DROP COLUMN engine_deadline;
DROP TABLE engine_cursors;
