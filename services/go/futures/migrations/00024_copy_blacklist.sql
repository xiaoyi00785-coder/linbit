-- +goose Up
-- Lead-trader blacklist: a trader can block specific users from copying them.
CREATE TABLE IF NOT EXISTS fut_copy_blacklist (
	trader       text        NOT NULL,
	blocked_user text        NOT NULL,
	reason       text        NOT NULL DEFAULT '',
	created_at   timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (trader, blocked_user)
);
CREATE INDEX IF NOT EXISTS idx_fut_copy_blacklist_trader ON fut_copy_blacklist (trader);

-- +goose Down
DROP TABLE IF EXISTS fut_copy_blacklist;
