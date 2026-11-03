-- +goose Up
-- Daily solvency reconciliation: on-chain holdings >= user balances + MM balance.
CREATE TABLE solvency_reports (
    id         BIGSERIAL PRIMARY KEY,
    trigger    TEXT NOT NULL,              -- daily | manual:<admin>
    ok         BOOLEAN NOT NULL,
    report     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX solvency_reports_created ON solvency_reports (created_at DESC);

-- +goose Down
DROP TABLE solvency_reports;
