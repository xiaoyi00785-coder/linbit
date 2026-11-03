-- +goose Up
-- Idempotency gate for wallet-driven ledger operations (deposit crediting and
-- withdrawal completion). Each ref may be applied at most once: the wallet
-- retries scans/processing passes, and re-posting must be a no-op.
CREATE TABLE wallet_ops (
    ref        TEXT PRIMARY KEY,
    op         TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    asset      TEXT NOT NULL,
    amount     NUMERIC(36,18) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE wallet_ops;
