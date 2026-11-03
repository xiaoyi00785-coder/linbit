-- +goose Up
-- Periodic insurance-fund balance snapshots for the public history page.
CREATE TABLE fut_insurance_snapshots (
    ts      TIMESTAMPTZ NOT NULL PRIMARY KEY,
    asset   TEXT NOT NULL DEFAULT 'USDT',
    balance NUMERIC(36,18) NOT NULL,
    equity  NUMERIC(36,18) NOT NULL
);

-- +goose Down
DROP TABLE fut_insurance_snapshots;
