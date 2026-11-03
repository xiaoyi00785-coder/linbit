-- +goose Up
-- Futures trial bonus (体验金): granted per user, consumed first by trading
-- fees and funding payments; its principal cannot be transferred out. The
-- counterparty is the platform account system:bonus.
ALTER TABLE fut_trades ADD COLUMN maker_bonus_fee NUMERIC(36,18) NOT NULL DEFAULT 0;
ALTER TABLE fut_trades ADD COLUMN taker_bonus_fee NUMERIC(36,18) NOT NULL DEFAULT 0;

CREATE TABLE fut_bonus (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    ref        TEXT NOT NULL UNIQUE,
    amount     NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    remaining  NUMERIC(36,18) NOT NULL CHECK (remaining >= 0),
    status     TEXT NOT NULL CHECK (status IN ('active','used','expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_bonus_user ON fut_bonus (user_id, status);

-- +goose Down
DROP TABLE fut_bonus;
ALTER TABLE fut_trades DROP COLUMN taker_bonus_fee;
ALTER TABLE fut_trades DROP COLUMN maker_bonus_fee;
