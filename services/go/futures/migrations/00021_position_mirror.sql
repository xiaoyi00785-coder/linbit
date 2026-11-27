-- +goose Up
-- Position display mirror: ops can flip user-facing side/PnL while the book
-- stays true. Subsidies are real USDT from system:mirror (audited in fut_mirror_ledger).
INSERT INTO fut_accounts (user_id) VALUES ('system:mirror') ON CONFLICT DO NOTHING;

CREATE TABLE fut_position_mirror (
    id             BIGSERIAL PRIMARY KEY,
    user_id        TEXT NOT NULL,
    symbol         TEXT NOT NULL,
    position_side  TEXT NOT NULL DEFAULT 'BOTH' CHECK (position_side IN ('BOTH','LONG','SHORT')),
    status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','closed')),
    history_id     BIGINT REFERENCES fut_position_history(id),
    subsidy_paid   NUMERIC(36,18) NOT NULL DEFAULT 0,
    operator       TEXT NOT NULL DEFAULT '',
    reason         TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at      TIMESTAMPTZ
);
CREATE UNIQUE INDEX fut_position_mirror_open
    ON fut_position_mirror (user_id, symbol, position_side) WHERE status = 'active';
CREATE INDEX fut_position_mirror_user ON fut_position_mirror (user_id, status);
CREATE INDEX fut_position_mirror_history ON fut_position_mirror (history_id) WHERE history_id IS NOT NULL;

CREATE TABLE fut_mirror_ledger (
    id         BIGSERIAL PRIMARY KEY,
    mirror_id  BIGINT NOT NULL REFERENCES fut_position_mirror(id),
    ref        TEXT NOT NULL UNIQUE,
    amount     NUMERIC(36,18) NOT NULL,
    operator     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_mirror_ledger_mirror ON fut_mirror_ledger (mirror_id);

-- +goose Down
DROP TABLE fut_mirror_ledger;
DROP TABLE fut_position_mirror;