-- +goose Up
-- Monotonic cursor over wallet_ops so the growth/earn services can follow
-- deposits and futures transfers.
ALTER TABLE wallet_ops ADD COLUMN seq BIGSERIAL;
CREATE UNIQUE INDEX wallet_ops_seq ON wallet_ops (seq);

-- +goose Down
DROP INDEX wallet_ops_seq;
ALTER TABLE wallet_ops DROP COLUMN seq;
