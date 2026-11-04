-- +goose Up
-- 站内客服工单: one ticket per issue, a message thread, and image attachments
-- (bytes live in the ticket attachment store, like avatars; rows here own them).
CREATE TABLE support_tickets (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id),
    category            TEXT NOT NULL CHECK (category IN ('account','deposit_withdraw','trade','kyc','security','other')),
    subject             TEXT NOT NULL CHECK (subject <> ''),
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','replied','closed')),
    assignee_id         BIGINT,
    assignee_name       TEXT,
    last_reply_by       TEXT NOT NULL DEFAULT 'user' CHECK (last_reply_by IN ('user','support')),
    last_reply_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_support_at     TIMESTAMPTZ,
    user_read_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at           TIMESTAMPTZ,
    closed_by           TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_tickets_user ON support_tickets (user_id, id DESC);
CREATE INDEX support_tickets_status ON support_tickets (status, last_reply_at DESC);
CREATE INDEX support_tickets_assignee ON support_tickets (assignee_id) WHERE assignee_id IS NOT NULL;

CREATE TABLE support_ticket_messages (
    id           BIGSERIAL PRIMARY KEY,
    ticket_id    BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    author_type  TEXT NOT NULL CHECK (author_type IN ('user','support')),
    author_id    BIGINT NOT NULL,
    author_name  TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL CHECK (body <> ''),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_ticket_messages_ticket ON support_ticket_messages (ticket_id, id);
CREATE INDEX support_ticket_messages_rate ON support_ticket_messages (author_id, created_at) WHERE author_type = 'user';

CREATE TABLE support_attachments (
    id          TEXT PRIMARY KEY,            -- public id: <random>.<ext>
    user_id     BIGINT NOT NULL REFERENCES users(id),
    storage_key TEXT NOT NULL,
    mime        TEXT NOT NULL,
    size_bytes  INT NOT NULL CHECK (size_bytes > 0),
    message_id  BIGINT REFERENCES support_ticket_messages(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_attachments_user ON support_attachments (user_id, created_at);
CREATE INDEX support_attachments_message ON support_attachments (message_id) WHERE message_id IS NOT NULL;

-- +goose Down
DROP TABLE support_attachments;
DROP TABLE support_ticket_messages;
DROP TABLE support_tickets;
