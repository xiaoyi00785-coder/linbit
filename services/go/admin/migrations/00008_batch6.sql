-- +goose Up
-- batch6 (t84): App 版本/升级配置、可编辑文案、App 推送任务记录。
CREATE TABLE IF NOT EXISTS admin_app_versions (
    platform        TEXT PRIMARY KEY CHECK (platform IN ('ios','android')),
    latest_version  TEXT NOT NULL,
    latest_build    INT  NOT NULL DEFAULT 0,
    min_version     TEXT NOT NULL DEFAULT '',
    download_url    TEXT NOT NULL DEFAULT '',
    release_notes   TEXT NOT NULL DEFAULT '',
    enabled         BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by      TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS admin_content_docs (
    key         TEXT PRIMARY KEY,
    title       TEXT NOT NULL DEFAULT '',
    format      TEXT NOT NULL DEFAULT 'markdown' CHECK (format IN ('markdown','json')),
    body        TEXT NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by  TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS admin_push_jobs (
    id            BIGSERIAL PRIMARY KEY,
    title         TEXT NOT NULL,
    body          TEXT NOT NULL,
    target        TEXT NOT NULL,           -- all | uid:<uid>
    platform      TEXT NOT NULL DEFAULT 'all',
    device_count  INT  NOT NULL DEFAULT 0,
    status        TEXT NOT NULL,           -- no_credentials | queued | sent | failed
    note          TEXT NOT NULL DEFAULT '',
    created_by    TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS admin_push_jobs;
DROP TABLE IF EXISTS admin_content_docs;
DROP TABLE IF EXISTS admin_app_versions;
