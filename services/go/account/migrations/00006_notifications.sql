-- +goose Up
CREATE TABLE notifications (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id),
    kind         TEXT NOT NULL CHECK (kind IN ('system','trade','activity','security')),
    title        TEXT NOT NULL,
    body         TEXT NOT NULL,
    read_at      TIMESTAMPTZ,
    email_status TEXT NOT NULL DEFAULT 'none' CHECK (email_status IN ('none','pending','sent','failed','no_address')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user ON notifications (user_id, id DESC);
CREATE INDEX notifications_unread ON notifications (user_id) WHERE read_at IS NULL;

-- Email channel preferences; security notices are always emailed.
CREATE TABLE notification_prefs (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id),
    email_system   BOOLEAN NOT NULL DEFAULT TRUE,
    email_trade    BOOLEAN NOT NULL DEFAULT TRUE,
    email_activity BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE notification_prefs;
DROP TABLE notifications;
