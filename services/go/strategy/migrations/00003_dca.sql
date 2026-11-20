-- +goose Up
CREATE TABLE dca_plans (
    id               BIGSERIAL PRIMARY KEY,
    user_id          TEXT NOT NULL,
    symbol           TEXT NOT NULL,
    base_asset       TEXT NOT NULL,
    quote_asset      TEXT NOT NULL,
    side             TEXT NOT NULL CHECK (side IN ('buy','sell')),
    -- amount spent (buy, in quote) or sold (sell, in base) each execution
    amount_per_run   NUMERIC(36,18) NOT NULL,
    interval_seconds INT NOT NULL CHECK (interval_seconds >= 60),
    trigger_type     TEXT NOT NULL DEFAULT 'time' CHECK (trigger_type IN ('time','price')),
    trigger_price    NUMERIC(36,18),
    start_at         TIMESTAMPTZ NOT NULL,
    end_at           TIMESTAMPTZ,
    next_run_at      TIMESTAMPTZ NOT NULL,
    status           TEXT NOT NULL CHECK (status IN ('active','paused','stopped')) DEFAULT 'active',
    stop_reason      TEXT,
    total_runs       INT NOT NULL DEFAULT 0,
    total_base       NUMERIC(36,18) NOT NULL DEFAULT 0,  -- base asset accumulated (bought) or sold
    total_quote      NUMERIC(36,18) NOT NULL DEFAULT 0,  -- quote spent (buy) or received (sell)
    total_fee_base   NUMERIC(36,18) NOT NULL DEFAULT 0,
    total_fee_quote  NUMERIC(36,18) NOT NULL DEFAULT 0,
    last_run_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at       TIMESTAMPTZ
);
CREATE INDEX dca_plans_user ON dca_plans (user_id, id DESC);
CREATE INDEX dca_plans_due  ON dca_plans (next_run_at) WHERE status = 'active';

CREATE TABLE dca_executions (
    id              BIGSERIAL PRIMARY KEY,
    plan_id         BIGINT NOT NULL REFERENCES dca_plans(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy','sell')),
    order_id        TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL CHECK (status IN ('pending','filled','canceled','failed')) DEFAULT 'pending',
    requested_quote NUMERIC(36,18) NOT NULL DEFAULT 0,
    requested_base  NUMERIC(36,18) NOT NULL DEFAULT 0,
    filled_base     NUMERIC(36,18) NOT NULL DEFAULT 0,
    filled_quote    NUMERIC(36,18) NOT NULL DEFAULT 0,
    fee_base        NUMERIC(36,18) NOT NULL DEFAULT 0,
    fee_quote       NUMERIC(36,18) NOT NULL DEFAULT 0,
    avg_price       NUMERIC(36,18),
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at         TIMESTAMPTZ
);
CREATE INDEX dca_exec_plan ON dca_executions (plan_id, id DESC);
CREATE INDEX dca_exec_user ON dca_executions (user_id, id DESC);

-- +goose Down
DROP TABLE dca_executions;
DROP TABLE dca_plans;
