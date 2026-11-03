-- +goose Up
-- Idempotency ledger for trade settlement: one row per matched trade. The
-- PRIMARY KEY makes re-delivering the same tradeId a no-op, so settlement is
-- exactly-once even if the order service retries.
CREATE TABLE settlements (
    trade_id   TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE settlements;
