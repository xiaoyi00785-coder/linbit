-- +goose Up
-- External login subjects. No identities are seeded.
CREATE TABLE oauth_identities (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE UNIQUE INDEX oauth_identities_user_provider ON oauth_identities (user_id, provider);

-- +goose Down
DROP TABLE oauth_identities;
