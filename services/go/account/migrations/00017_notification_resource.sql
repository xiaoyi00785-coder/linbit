-- +goose Up
-- Optional typed target for a notification. NULL means no reliable location
-- (historical rows and non-ticket notices). Only support_ticket is allowed.
ALTER TABLE notifications ADD COLUMN resource_type TEXT;
ALTER TABLE notifications ADD COLUMN resource_id TEXT;
ALTER TABLE notifications ADD CONSTRAINT notifications_resource_chk CHECK (
    (resource_type IS NULL AND resource_id IS NULL)
    OR (resource_type = 'support_ticket' AND resource_id ~ '^[0-9]+$')
);

-- +goose Down
ALTER TABLE notifications DROP CONSTRAINT notifications_resource_chk;
ALTER TABLE notifications DROP COLUMN resource_id;
ALTER TABLE notifications DROP COLUMN resource_type;
