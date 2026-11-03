-- +goose Up
-- 质押借币: USDT loans against BTC/ETH collateral, hourly interest.
CREATE TABLE e_loans (
    id                BIGSERIAL PRIMARY KEY,
    uid               TEXT NOT NULL,
    asset             TEXT NOT NULL,                 -- borrowed (USDT)
    collateral_asset  TEXT NOT NULL,
    term              TEXT NOT NULL CHECK (term IN ('flexible','7d','30d')),
    hourly_rate       NUMERIC(20,12) NOT NULL,
    initial_principal NUMERIC(36,8) NOT NULL,
    initial_collateral NUMERIC(36,8) NOT NULL,
    principal         NUMERIC(36,8) NOT NULL,        -- outstanding
    collateral        NUMERIC(36,8) NOT NULL,        -- locked
    interest          NUMERIC(36,8) NOT NULL DEFAULT 0, -- accrued, unpaid
    interest_paid     NUMERIC(36,8) NOT NULL DEFAULT 0,
    hours_charged     INT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL CHECK (status IN ('pending','active','repaid','liquidated','failed')),
    warned_at         TIMESTAMPTZ,
    started_at        TIMESTAMPTZ NOT NULL,
    due_at            TIMESTAMPTZ,                   -- NULL for flexible
    closed_at         TIMESTAMPTZ
);
CREATE INDEX e_loans_uid ON e_loans (uid, status);

-- Order records: borrow | repay | collateral | renew | liquidate.
CREATE TABLE e_loan_events (
    id        BIGSERIAL PRIMARY KEY,
    loan_id   BIGINT NOT NULL REFERENCES e_loans(id),
    uid       TEXT NOT NULL,
    kind      TEXT NOT NULL,
    amount    NUMERIC(36,8) NOT NULL DEFAULT 0,     -- loan asset (principal part)
    interest  NUMERIC(36,8) NOT NULL DEFAULT 0,
    penalty   NUMERIC(36,8) NOT NULL DEFAULT 0,
    collateral NUMERIC(36,8) NOT NULL DEFAULT 0,    -- collateral asset moved
    price     NUMERIC(36,8),
    ltv       NUMERIC(20,8),
    detail    JSONB NOT NULL DEFAULT '{}',
    at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX e_loan_events_uid ON e_loan_events (uid, kind, at);

-- +goose Down
DROP TABLE e_loan_events;
DROP TABLE e_loans;
