-- +goose Up
-- Per-user override of the platform deposit_account setting.
-- No rows: deposits keep using the platform default. No balances are moved.
CREATE TABLE user_deposit_accounts (
    user_id    TEXT PRIMARY KEY,
    account    TEXT NOT NULL CHECK (account IN ('funding', 'spot')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_deposit_accounts;
