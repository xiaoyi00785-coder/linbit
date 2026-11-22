-- +goose Up
-- 1:1 customer-service (客服) conversations. Live WS channel = "cs:{pub_token}"
-- (unguessable; possession-based so the public /ws needs no per-user auth).
CREATE TABLE cs_conversations (
    id           BIGSERIAL PRIMARY KEY,
    pub_token    TEXT NOT NULL UNIQUE,              -- random id used for the WS channel + guest URLs
    uid          TEXT NOT NULL DEFAULT '',          -- '' = not-yet-logged-in visitor
    guest_key    TEXT NOT NULL DEFAULT '',          -- opaque client id for anonymous visitors
    ip           TEXT NOT NULL DEFAULT '',          -- creator IP (guest anti-abuse)
    subject      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'open'
                 CHECK (status IN ('open','pending','assigned','resolved','closed')),
    assignee     TEXT NOT NULL DEFAULT '',          -- staff/agent id, '' = unassigned
    last_msg_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    unread_user  INT NOT NULL DEFAULT 0,            -- staff msgs the user has not read
    unread_staff INT NOT NULL DEFAULT 0,            -- user msgs staff has not read
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cs_conv_uid      ON cs_conversations (uid, last_msg_at DESC) WHERE uid <> '';
CREATE INDEX cs_conv_guest    ON cs_conversations (guest_key) WHERE guest_key <> '';
CREATE INDEX cs_conv_status   ON cs_conversations (status, last_msg_at DESC);
CREATE INDEX cs_conv_assignee ON cs_conversations (assignee, last_msg_at DESC) WHERE assignee <> '';

CREATE TABLE cs_messages (
    id          BIGSERIAL PRIMARY KEY,
    conv_id     BIGINT NOT NULL REFERENCES cs_conversations(id) ON DELETE CASCADE,
    sender      TEXT NOT NULL,                      -- uid | guest_key | staff id
    sender_kind TEXT NOT NULL CHECK (sender_kind IN ('user','staff','system')),
    client_id   TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text','image')),
    text        TEXT NOT NULL DEFAULT '',
    image_url   TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','deleted')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cs_msg_conv ON cs_messages (conv_id, id DESC);
-- Idempotency only applies to client-supplied ids (staff/system send '').
CREATE UNIQUE INDEX cs_msg_idem ON cs_messages (sender, client_id) WHERE client_id <> '';

-- +goose Down
DROP TABLE cs_messages;
DROP TABLE cs_conversations;
