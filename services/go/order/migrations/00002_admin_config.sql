-- +goose Up

-- Runtime fee rates managed by the admin service (dual-approved changes are
-- applied here when they take effect). Empty = env defaults.
CREATE TABLE fee_rates (
    key        TEXT PRIMARY KEY,            -- spot_maker | spot_taker
    rate       NUMERIC(10,8) NOT NULL CHECK (rate >= 0 AND rate < 0.1),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT
);

-- +goose Down
DROP TABLE fee_rates;
