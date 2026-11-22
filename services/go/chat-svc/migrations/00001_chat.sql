-- +goose Up
-- chat-svc owns the trading chat-room tables (moved out of market-data). Uses
-- IF NOT EXISTS so it co-exists safely with a database where market-data's
-- 00005_chat.sql already created them (shared Postgres during the migration).
CREATE TABLE IF NOT EXISTS chat_config (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    cfg        JSONB NOT NULL DEFAULT '{}',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO chat_config (id, cfg) VALUES (1, '{}') ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS chat_messages (
    id             BIGSERIAL PRIMARY KEY,
    room           TEXT NOT NULL,
    uid            TEXT NOT NULL,
    client_id      TEXT NOT NULL,
    text           TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','deleted')),
    deleted_by     TEXT NOT NULL DEFAULT '',
    deleted_reason TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (uid, client_id)
);
CREATE INDEX IF NOT EXISTS chat_messages_room_id ON chat_messages (room, id DESC);
CREATE INDEX IF NOT EXISTS chat_messages_uid_created ON chat_messages (uid, created_at DESC);

CREATE TABLE IF NOT EXISTS chat_sanctions (
    uid         TEXT PRIMARY KEY,
    muted_until TIMESTAMPTZ,
    banned      BOOLEAN NOT NULL DEFAULT false,
    reason      TEXT NOT NULL DEFAULT '',
    updated_by  TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_reports (
    id          BIGSERIAL PRIMARY KEY,
    message_id  BIGINT NOT NULL REFERENCES chat_messages(id),
    reporter    TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','dismissed')),
    handled_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    handled_at  TIMESTAMPTZ,
    UNIQUE (message_id, reporter)
);
CREATE INDEX IF NOT EXISTS chat_reports_status ON chat_reports (status, id DESC);

-- +goose Down
-- No-op down: these tables may be shared; chat-svc does not drop them.
SELECT 1;
