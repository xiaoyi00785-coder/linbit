-- +goose Up
-- Admin-managed loan products: per-term rate / status / lending quota and
-- the accepted collateral assets with their per-user caps. Rows are seeded
-- from the LOAN_* env defaults on first start; afterwards the tables are the
-- source of truth (rates change only through the admin dual approval).
CREATE TABLE e_loan_terms (
    term        TEXT PRIMARY KEY CHECK (term IN ('flexible','7d','30d')),
    hourly_rate NUMERIC(20,12) NOT NULL CHECK (hourly_rate >= 0),
    status      TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','offline')),
    total_quota NUMERIC(36,8),          -- NULL = unlimited; outstanding principal cap for the term
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE e_loan_collaterals (
    asset      TEXT PRIMARY KEY,
    user_cap   NUMERIC(36,8) NOT NULL CHECK (user_cap > 0),
    status     TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','offline')),
    sort       INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE e_loan_collaterals;
DROP TABLE e_loan_terms;
