-- +goose Up
-- Funding wallet: /assets/transfer now moves between funding, spot and futures.
-- wallet = the asset-service wallet on the non-futures side of an in/out leg
-- (legacy rows: spot). direction 'internal' = funding <-> spot (no futures leg,
-- any asset) with explicit from_account/to_account.
ALTER TABLE fut_transfers ADD COLUMN wallet TEXT NOT NULL DEFAULT 'spot' CHECK (wallet IN ('spot','funding'));
ALTER TABLE fut_transfers ADD COLUMN from_account TEXT;
ALTER TABLE fut_transfers ADD COLUMN to_account TEXT;
ALTER TABLE fut_transfers DROP CONSTRAINT IF EXISTS fut_transfers_direction_check;
ALTER TABLE fut_transfers ADD CONSTRAINT fut_transfers_direction_check CHECK (direction IN ('in','out','internal'));

-- Per-user futures fee overrides (admin). NULL keeps the contract rate.
CREATE TABLE fut_fee_overrides (
    user_id    TEXT PRIMARY KEY,
    maker_fee  NUMERIC(36,18),
    taker_fee  NUMERIC(36,18),
    note       TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE fut_fee_overrides;
DELETE FROM fut_transfers WHERE direction = 'internal';
ALTER TABLE fut_transfers DROP CONSTRAINT fut_transfers_direction_check;
ALTER TABLE fut_transfers ADD CONSTRAINT fut_transfers_direction_check CHECK (direction IN ('in','out'));
ALTER TABLE fut_transfers DROP COLUMN to_account;
ALTER TABLE fut_transfers DROP COLUMN from_account;
ALTER TABLE fut_transfers DROP COLUMN wallet;
