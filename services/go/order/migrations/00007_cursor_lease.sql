-- +goose Up
-- One consumer replica at a time owns a symbol's result-stream cursor.
-- lease_until is the instant the lease expires; after that another replica
-- may claim the row. Asset settlement is idempotent, so a brief overlap only
-- repeats work — the lease exists to avoid that overlap in the steady state.
ALTER TABLE engine_cursors ADD COLUMN lease_owner TEXT;
ALTER TABLE engine_cursors ADD COLUMN lease_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE engine_cursors DROP COLUMN lease_until;
ALTER TABLE engine_cursors DROP COLUMN lease_owner;
