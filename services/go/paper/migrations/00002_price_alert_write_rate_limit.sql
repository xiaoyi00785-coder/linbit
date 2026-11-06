-- +goose Up
-- One durable bucket per user coordinates CRUD throttling across replicas and restarts.
CREATE TABLE price_alert_write_rate_limits (
 user_id TEXT PRIMARY KEY,
 window_started_at TIMESTAMPTZ NOT NULL,
 request_count INTEGER NOT NULL CHECK (request_count >= 0)
);

-- +goose Down
DROP TABLE price_alert_write_rate_limits;
