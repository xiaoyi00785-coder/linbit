-- +goose Up
-- Invite code typed at registration (normalized, may be ''); resolved to an
-- inviter by the growth service. Registration never fails on a bad code.
ALTER TABLE users ADD COLUMN invite_code TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN invite_code;
