-- +goose Up
-- Launchpad / 新币申购: users commit a base asset (USDT or platform coin)
-- during an open window; at settle_at every subscription's average snapshot
-- balance decides the pro-rata allocation of the new token; at distribute_at
-- the won amount is credited to the user's spot wallet in the new token and
-- the unused part of the lock is refunded.

CREATE TABLE launchpad_projects (
    id             TEXT PRIMARY KEY,
    symbol         TEXT NOT NULL,                             -- the token being sold (e.g. NEWCOIN)
    name           TEXT NOT NULL,                             -- display name
    logo           TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    base_asset     TEXT NOT NULL,                             -- lock asset (USDT / LBT)
    price          NUMERIC(36,8) NOT NULL,                    -- new-token price in base_asset
    total_supply   NUMERIC(36,8) NOT NULL,                    -- max tokens offered
    min_sub        NUMERIC(36,8) NOT NULL DEFAULT 0,          -- per-user min lock (base asset)
    max_sub        NUMERIC(36,8),                             -- per-user max lock (null = no cap)
    start_at       TIMESTAMPTZ NOT NULL,                      -- subscription window open
    end_at         TIMESTAMPTZ NOT NULL,                      -- subscription window close
    settle_at      TIMESTAMPTZ NOT NULL,                      -- allocation computed
    distribute_at  TIMESTAMPTZ NOT NULL,                      -- tokens credited, refunds paid
    status         TEXT NOT NULL DEFAULT 'upcoming'
                   CHECK (status IN ('upcoming','open','computing','settled','distributed','cancelled')),
    rules_md       TEXT NOT NULL DEFAULT '',                  -- announcement markdown
    allocated      NUMERIC(36,8) NOT NULL DEFAULT 0,          -- running total of allocated tokens
    raised         NUMERIC(36,8) NOT NULL DEFAULT 0,          -- running total of used base asset
    settled_at     TIMESTAMPTZ,
    distributed_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    sort           INT NOT NULL DEFAULT 0
);
CREATE INDEX launchpad_projects_status ON launchpad_projects (status);
CREATE INDEX launchpad_projects_time   ON launchpad_projects (end_at, settle_at, distribute_at);

-- One row per user per project. Partial cancels reduce `amount` while the
-- window is still open; snapshots keep their historical values.
CREATE TABLE launchpad_subscriptions (
    id             BIGSERIAL PRIMARY KEY,
    uid            TEXT NOT NULL,
    project_id     TEXT NOT NULL REFERENCES launchpad_projects(id),
    amount         NUMERIC(36,8) NOT NULL,                    -- current locked base amount
    locked_at      TIMESTAMPTZ NOT NULL,                      -- first-lock time
    avg_base       NUMERIC(36,8),                             -- averaged across snapshots at settle
    result_amount  NUMERIC(36,8),                             -- won new tokens at settle
    used_base      NUMERIC(36,8),                             -- base actually consumed by allocation
    refund_amount  NUMERIC(36,8),                             -- base refunded (locked - used_base)
    status         TEXT NOT NULL DEFAULT 'holding'
                   CHECK (status IN ('holding','cancelled','settled','distributed')),
    settled_at     TIMESTAMPTZ,
    distributed_at TIMESTAMPTZ,
    UNIQUE (uid, project_id)
);
CREATE INDEX launchpad_subscriptions_project ON launchpad_subscriptions (project_id, status);
CREATE INDEX launchpad_subscriptions_user    ON launchpad_subscriptions (uid, id DESC);

-- Hourly snapshot of each subscription's locked balance during the window.
-- hour_bucket is unix/3600 (UTC). One row per (user, project, hour).
CREATE TABLE launchpad_snapshots (
    id            BIGSERIAL PRIMARY KEY,
    uid           TEXT NOT NULL,
    project_id    TEXT NOT NULL REFERENCES launchpad_projects(id),
    hour_bucket   INT NOT NULL,
    base_balance  NUMERIC(36,8) NOT NULL,
    taken_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (uid, project_id, hour_bucket)
);
CREATE INDEX launchpad_snapshots_project ON launchpad_snapshots (project_id);

-- Per-user audit trail (subscribe / cancel / settle / distribute / refund).
CREATE TABLE launchpad_events (
    id          BIGSERIAL PRIMARY KEY,
    uid         TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('subscribe','cancel','settle','distribute','refund')),
    asset       TEXT NOT NULL,
    amount      NUMERIC(36,8) NOT NULL,
    detail      JSONB NOT NULL DEFAULT '{}',
    at          TIMESTAMPTZ NOT NULL
);
CREATE INDEX launchpad_events_uid ON launchpad_events (uid, id DESC);

-- +goose Down
DROP TABLE launchpad_events;
DROP TABLE launchpad_snapshots;
DROP TABLE launchpad_subscriptions;
DROP TABLE launchpad_projects;
