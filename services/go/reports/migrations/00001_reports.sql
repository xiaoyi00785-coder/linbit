-- +goose Up
-- Reports / exports / notification subscriptions. Owned by the reports service;
-- it shares DATABASE_URL with every other service and reads source rows
-- (deposits, withdrawals, trades, internal_transfers, ledger_entries,
-- funding_balances) straight from the shared tables.
CREATE TABLE report_exports (
    id             BIGSERIAL PRIMARY KEY,
    job_id         TEXT NOT NULL UNIQUE,              -- public opaque id
    user_id        TEXT NOT NULL,
    kind           TEXT NOT NULL,                     -- trades|deposits|withdrawals|transfers|funding|ledger
    format         TEXT NOT NULL,                     -- csv|xlsx|pdf
    from_at        TIMESTAMPTZ,
    to_at          TIMESTAMPTZ,
    status         TEXT NOT NULL DEFAULT 'queued',    -- queued|running|ready|failed|expired
    file_path      TEXT,
    size_bytes     BIGINT,
    row_count      INT,
    download_token TEXT,                              -- random hex; verified for downloads
    error          TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ
);
CREATE INDEX idx_report_exports_user ON report_exports (user_id, created_at DESC);
CREATE INDEX idx_report_exports_queued ON report_exports (status, created_at) WHERE status IN ('queued', 'running');

-- Per-user to-account alerts: notify when a deposit matches asset (+ optional
-- chain / address / min amount) on the user's configured channel.
CREATE TABLE notification_subscriptions (
    id             BIGSERIAL PRIMARY KEY,
    user_id        TEXT NOT NULL,
    asset          TEXT NOT NULL,
    chain          TEXT NOT NULL DEFAULT '',          -- '' = any chain
    address_filter TEXT NOT NULL DEFAULT '',          -- '' = any address
    min_amount     NUMERIC(36,18),                    -- NULL = any amount
    channel        TEXT NOT NULL CHECK (channel IN ('push','email','both')),
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notif_subs_user ON notification_subscriptions (user_id);
CREATE INDEX idx_notif_subs_asset ON notification_subscriptions (asset, active);

-- Watermark for the deposit-event watcher: deposits.id up to which we have
-- already processed newly-completed deposits for subscription fan-out AND for
-- the account-events WebSocket stream.
CREATE TABLE report_cursors (
    name       TEXT PRIMARY KEY,
    position   BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO report_cursors (name, position) VALUES
    ('deposits_watermark', 0),
    ('withdrawals_watermark', 0),
    ('trades_watermark', 0)
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE report_cursors;
DROP TABLE notification_subscriptions;
DROP TABLE report_exports;
