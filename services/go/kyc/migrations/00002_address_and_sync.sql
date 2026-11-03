-- +goose Up

-- Advanced (level 2) verification requires a proof-of-address document.
ALTER TABLE kyc_applications ADD COLUMN proof_of_address_doc_id BIGINT REFERENCES kyc_documents(id);

-- Persisted outbox for syncing an approved level to the account service. A row
-- exists while the sync is outstanding; failures are retried with exponential
-- backoff (next_attempt_at) until the account service accepts it.
CREATE TABLE kyc_level_sync (
    user_id         TEXT PRIMARY KEY,
    level           INT NOT NULL,
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kyc_level_sync_due ON kyc_level_sync (next_attempt_at);

-- +goose Down
DROP TABLE kyc_level_sync;
ALTER TABLE kyc_applications DROP COLUMN proof_of_address_doc_id;
