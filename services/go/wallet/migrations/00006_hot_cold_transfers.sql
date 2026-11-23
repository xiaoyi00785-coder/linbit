-- +goose Up
-- Manual hot → cold transfers initiated from the admin console.
-- Destination is always the pre-configured cold address (never client-supplied).
-- Status: pending_review → approved → processing → completed
--                     ↘ rejected | canceled ; processing ↘ failed

CREATE TABLE hot_cold_transfers (
    id                 BIGSERIAL PRIMARY KEY,
    asset              TEXT NOT NULL,
    network            TEXT NOT NULL,
    mode               TEXT NOT NULL,
    amount             NUMERIC(36,18) NOT NULL,
    reserve_amount     NUMERIC(36,18) NOT NULL DEFAULT 0,
    destination        TEXT NOT NULL,
    reason             TEXT NOT NULL,
    initiated_by       BIGINT NOT NULL,
    initiated_by_name  TEXT NOT NULL DEFAULT '',
    approved_by        BIGINT,
    approved_by_name   TEXT NOT NULL DEFAULT '',
    approved_at        TIMESTAMPTZ,
    rejected_by        BIGINT,
    rejected_by_name   TEXT NOT NULL DEFAULT '',
    rejected_at        TIMESTAMPTZ,
    reject_reason      TEXT NOT NULL DEFAULT '',
    idempotency_key    TEXT NOT NULL,
    tx_hash            TEXT NOT NULL DEFAULT '',
    broadcast_height   BIGINT NOT NULL DEFAULT 0,
    confirmations      BIGINT NOT NULL DEFAULT 0,
    status             TEXT NOT NULL,
    error              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT hot_cold_transfers_mode_chk CHECK (mode IN ('amount', 'reserve')),
    CONSTRAINT hot_cold_transfers_status_chk CHECK (status IN (
        'pending_review', 'approved', 'processing', 'completed', 'failed', 'rejected', 'canceled'
    ))
);

CREATE UNIQUE INDEX uq_hot_cold_transfers_idempotency
    ON hot_cold_transfers (idempotency_key);

-- Only one in-flight hot→cold transfer per asset+network (prevents double submit).
CREATE UNIQUE INDEX uq_hot_cold_transfers_inflight
    ON hot_cold_transfers (asset, network)
    WHERE status IN ('pending_review', 'approved', 'processing');

CREATE INDEX idx_hot_cold_transfers_status_created
    ON hot_cold_transfers (status, created_at DESC);

-- +goose Down
DROP TABLE hot_cold_transfers;
