-- +goose Up

-- Uploaded KYC documents. A document is owned by the uploader (user_id) and
-- carries its side (front | selfie | back). file_key is the ObjectStorage key;
-- bytes are never public and are read back only through the admin controlled
-- route. A document is referenced by at most one application at submit time.
CREATE TABLE kyc_documents (
    id           BIGSERIAL PRIMARY KEY,
    user_id      TEXT NOT NULL,
    side         TEXT NOT NULL,              -- front | selfie | back
    file_key     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kyc_documents_user ON kyc_documents (user_id);

-- KYC applications and their review state machine.
--   pending -> approved      (admin approve; syncs account.kyc_level)
--   pending -> rejected      (admin reject with a reason; user may re-submit)
-- level 1 = basic, level 2 = advanced (requires an approved level-1 first).
CREATE TABLE kyc_applications (
    id            BIGSERIAL PRIMARY KEY,
    user_id       TEXT NOT NULL,
    level         INT NOT NULL CHECK (level IN (1, 2)),
    status        TEXT NOT NULL,             -- pending | approved | rejected
    first_name    TEXT NOT NULL,
    last_name     TEXT NOT NULL,
    country       TEXT NOT NULL,             -- ISO 3166-1 alpha-2
    id_type       TEXT NOT NULL,             -- id_card | passport | driver_license
    id_number     TEXT NOT NULL,
    date_of_birth DATE NOT NULL,
    front_doc_id  BIGINT REFERENCES kyc_documents(id),
    selfie_doc_id BIGINT REFERENCES kyc_documents(id),
    back_doc_id   BIGINT REFERENCES kyc_documents(id),
    reason        TEXT,
    submitted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kyc_applications_user ON kyc_applications (user_id, created_at DESC);
CREATE INDEX idx_kyc_applications_status ON kyc_applications (status, created_at DESC);

-- At most one pending application per user (enforces the "single in-flight" rule).
CREATE UNIQUE INDEX uq_kyc_one_pending ON kyc_applications (user_id) WHERE status = 'pending';

-- An identity document (country + type + number) may back at most one APPROVED
-- user — it cannot be reused by anyone else once approved.
CREATE UNIQUE INDEX uq_kyc_id_number_approved
    ON kyc_applications (country, id_type, id_number, level) WHERE status = 'approved';

-- +goose Down
DROP TABLE kyc_applications;
DROP TABLE kyc_documents;
