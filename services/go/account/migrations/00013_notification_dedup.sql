-- +goose Up
ALTER TABLE notifications ADD COLUMN dedup_key TEXT;
ALTER TABLE notifications ALTER COLUMN dedup_key DROP DEFAULT;
-- Existing authoritative schema uses BIGSERIAL user IDs; this sized index is
-- unique per user+producer event and NULL keeps all historical rows distinct.
CREATE UNIQUE INDEX notifications_user_dedup ON notifications (user_id, dedup_key) WHERE dedup_key IS NOT NULL;

-- +goose Down
DROP INDEX notifications_user_dedup;
ALTER TABLE notifications DROP COLUMN dedup_key;
