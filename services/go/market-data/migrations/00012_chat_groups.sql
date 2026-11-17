-- +goose Up
CREATE TABLE chat_folders (
    id         BIGSERIAL PRIMARY KEY,
    uid        TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (uid, name)
);
CREATE TABLE chat_members (
    uid        TEXT NOT NULL,
    room       TEXT NOT NULL,
    folder_id  BIGINT REFERENCES chat_folders(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (uid, room)
);
CREATE INDEX chat_members_uid ON chat_members (uid, created_at DESC);

-- +goose Down
DROP TABLE chat_members;
DROP TABLE chat_folders;
