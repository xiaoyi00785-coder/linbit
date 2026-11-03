-- +goose Up
-- Proof-of-reserves snapshots. Published by operations (one row per asset per
-- snapshot); served publicly as GET /market/proof-of-reserves (latest only).
-- Empty table = "not yet published" — the API says so instead of demo numbers.
CREATE TABLE por_snapshots (
    snapshot_at  TIMESTAMPTZ NOT NULL,
    asset        TEXT NOT NULL,
    liabilities  NUMERIC(38,18) NOT NULL CHECK (liabilities >= 0),
    reserves     NUMERIC(38,18) NOT NULL CHECK (reserves >= 0),
    PRIMARY KEY (snapshot_at, asset)
);

-- +goose Down
DROP TABLE por_snapshots;
