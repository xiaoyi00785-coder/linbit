-- +goose Up
-- Card product is unconfigured until a second operator approves a draft
-- that already names an issuer. This migration inserts no product and no application.
CREATE TABLE card_products (
    id                      INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    status                  TEXT NOT NULL DEFAULT 'unconfigured'
        CHECK (status IN ('unconfigured', 'draft', 'pending_review', 'published', 'rejected')),
    summary                 TEXT NOT NULL DEFAULT '',
    issuer_legal_name       TEXT NOT NULL DEFAULT '',
    issuer_registration_ref TEXT NOT NULL DEFAULT '',
    updated_by              TEXT NOT NULL DEFAULT '',
    submitted_by            TEXT NOT NULL DEFAULT '',
    reviewed_by             TEXT NOT NULL DEFAULT '',
    review_note             TEXT NOT NULL DEFAULT '',
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE card_applications (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status     TEXT NOT NULL CHECK (status IN ('submitted', 'in_review', 'approved', 'rejected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

-- +goose Down
DROP TABLE card_applications;
DROP TABLE card_products;
