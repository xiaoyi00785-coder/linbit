-- +goose Up
-- Spot inventory baseline per base asset. max_position caps only the NET
-- position the MM built by trading (current base - baseline), never the
-- seeded / treasury inventory. Baseline resolution (see baseline.go):
--   1. target_base  (admin-set, any network)
--   2. testnet/mock: sum of mm_testnet_seed credits for the asset (asset ledger)
--   3. start_base   (MM base balance captured the first time MM was started)
--   4. 0
CREATE TABLE mm_inventory_baseline (
    asset        TEXT PRIMARY KEY,
    target_base  NUMERIC(36,18),
    start_base   NUMERIC(36,18),
    start_at     TIMESTAMPTZ,
    updated_by   TEXT NOT NULL DEFAULT 'system',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (target_base IS NULL OR target_base >= 0),
    CHECK (start_base IS NULL OR start_base >= 0)
);

-- +goose Down
DROP TABLE mm_inventory_baseline;
