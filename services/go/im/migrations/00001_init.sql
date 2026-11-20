-- +goose Up
-- +goose StatementBegin

-- Conversations: a 1v1 chat or a group chat. For 1v1 the kind is 'c2c' and the
-- peer pair (uid_a, uid_b) with uid_a < uid_b lives in peer_lo/peer_hi so that
-- the uniqueness constraint catches duplicates regardless of insert order. For
-- groups the kind is 'group' and group_id references im_groups.
CREATE TABLE IF NOT EXISTS im_conversations (
    id          BIGSERIAL PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('c2c', 'group')),
    peer_lo     TEXT,
    peer_hi     TEXT,
    group_id    BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_msg_id BIGINT,
    last_msg_at TIMESTAMPTZ
);
-- Partial unique indexes: PostgreSQL treats NULLs as distinct in a plain
-- UNIQUE, so (kind, peer_lo, peer_hi, group_id) with NULL group_id never
-- collides and ON CONFLICT can't target it. These two partial indexes keep
-- c2c and group conversations uniquely keyed.
CREATE UNIQUE INDEX IF NOT EXISTS im_conv_c2c_unique ON im_conversations (peer_lo, peer_hi) WHERE kind = 'c2c';
CREATE UNIQUE INDEX IF NOT EXISTS im_conv_group_unique ON im_conversations (group_id) WHERE kind = 'group';
CREATE INDEX IF NOT EXISTS im_conv_c2c_lo_idx ON im_conversations (peer_lo);
CREATE INDEX IF NOT EXISTS im_conv_c2c_hi_idx ON im_conversations (peer_hi);

-- Per-user conversation state: unread count, last-read seq, mute, pin.
CREATE TABLE IF NOT EXISTS im_conversation_members (
    conversation_id BIGINT NOT NULL REFERENCES im_conversations(id) ON DELETE CASCADE,
    uid             TEXT NOT NULL,
    unread          INT NOT NULL DEFAULT 0,
    last_read_seq   BIGINT NOT NULL DEFAULT 0,
    muted           BOOLEAN NOT NULL DEFAULT false,
    pinned          BOOLEAN NOT NULL DEFAULT false,
    hidden          BOOLEAN NOT NULL DEFAULT false,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, uid)
);
CREATE INDEX IF NOT EXISTS im_cm_uid_idx ON im_conversation_members (uid);

-- Messages. seq is a per-conversation monotonic counter assigned at insert;
-- the (conversation_id, seq) pair acts as the shard key for history reads.
-- kind: text | image | audio | video | custom | system | redpacket | tip.
CREATE TABLE IF NOT EXISTS im_messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES im_conversations(id) ON DELETE CASCADE,
    seq             BIGINT NOT NULL,
    sender_uid      TEXT NOT NULL,
    kind            TEXT NOT NULL,
    payload         JSONB NOT NULL,
    mentions        JSONB NOT NULL DEFAULT '[]'::jsonb,
    reply_to        BIGINT,
    msg_random      BIGINT NOT NULL,
    recalled        BOOLEAN NOT NULL DEFAULT false,
    recalled_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, seq),
    UNIQUE (conversation_id, sender_uid, msg_random)
);
CREATE INDEX IF NOT EXISTS im_msg_conv_created_idx ON im_messages (conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS im_msg_sender_idx ON im_messages (sender_uid);

-- Groups.
CREATE TABLE IF NOT EXISTS im_groups (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL,
    avatar             TEXT,
    announcement       TEXT NOT NULL DEFAULT '',
    announcement_at    TIMESTAMPTZ,
    announcement_by    TEXT,
    owner_uid          TEXT NOT NULL,
    max_members        INT NOT NULL DEFAULT 500,
    mute_all           BOOLEAN NOT NULL DEFAULT false,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    member_count       INT NOT NULL DEFAULT 0,
    conversation_id    BIGINT REFERENCES im_conversations(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS im_groups_owner_idx ON im_groups (owner_uid);

-- Group members and their role / mute state.
CREATE TABLE IF NOT EXISTS im_group_members (
    group_id   BIGINT NOT NULL REFERENCES im_groups(id) ON DELETE CASCADE,
    uid        TEXT NOT NULL,
    role       TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')) DEFAULT 'member',
    muted_until TIMESTAMPTZ,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    nickname   TEXT,
    PRIMARY KEY (group_id, uid)
);
CREATE INDEX IF NOT EXISTS im_gm_uid_idx ON im_group_members (uid);

-- Redpackets: funds deducted at create, claimed atomically via Redis (with a
-- DB fallback), and anything still unclaimed at expiry is refunded.
CREATE TABLE IF NOT EXISTS im_redpackets (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES im_conversations(id) ON DELETE CASCADE,
    group_id        BIGINT REFERENCES im_groups(id) ON DELETE SET NULL,
    sender_uid      TEXT NOT NULL,
    asset           TEXT NOT NULL DEFAULT 'USDT',
    total_amount    NUMERIC(38, 18) NOT NULL,
    shares          INT NOT NULL,
    remaining_shares INT NOT NULL,
    remaining_amount NUMERIC(38, 18) NOT NULL,
    mode            TEXT NOT NULL CHECK (mode IN ('equal', 'random')),
    blessing        TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL CHECK (status IN ('open', 'closed', 'refunded')) DEFAULT 'open',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at       TIMESTAMPTZ,
    refunded_at     TIMESTAMPTZ,
    message_id      BIGINT REFERENCES im_messages(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS im_rp_conv_idx ON im_redpackets (conversation_id);
CREATE INDEX IF NOT EXISTS im_rp_status_expires_idx ON im_redpackets (status, expires_at);

-- One row per claim. Unique by (redpacket_id, uid) prevents double-claim at
-- the DB level even if the Redis guard is unavailable.
CREATE TABLE IF NOT EXISTS im_redpacket_claims (
    id           BIGSERIAL PRIMARY KEY,
    redpacket_id BIGINT NOT NULL REFERENCES im_redpackets(id) ON DELETE CASCADE,
    uid          TEXT NOT NULL,
    amount       NUMERIC(38, 18) NOT NULL,
    claimed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_best      BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (redpacket_id, uid)
);
CREATE INDEX IF NOT EXISTS im_rpc_uid_idx ON im_redpacket_claims (uid);

-- Online presence: the websocket hub writes last_seen_at / platform on connect,
-- and updates platform=null on disconnect. Clients may read peers' presence
-- through GET /im/users/{uid}/presence. Expired rows are harmless.
CREATE TABLE IF NOT EXISTS im_presence (
    uid          TEXT PRIMARY KEY,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    platform     TEXT
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS im_presence;
DROP TABLE IF EXISTS im_redpacket_claims;
DROP TABLE IF EXISTS im_redpackets;
DROP TABLE IF EXISTS im_group_members;
DROP TABLE IF EXISTS im_groups;
DROP TABLE IF EXISTS im_messages;
DROP TABLE IF EXISTS im_conversation_members;
DROP TABLE IF EXISTS im_conversations;
-- +goose StatementEnd
