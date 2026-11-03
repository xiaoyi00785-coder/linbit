-- +goose Up
-- Copy trading (futures only). Each follow owns a futures sub-account
-- fut_accounts.user_id = 'copy:<follow id>' that mirrors the trader.
CREATE TABLE fut_copy_traders (
    user_id       TEXT PRIMARY KEY,
    nickname      TEXT NOT NULL,
    bio           TEXT NOT NULL DEFAULT '',
    profit_share  NUMERIC(6,4) NOT NULL CHECK (profit_share >= 0 AND profit_share <= 0.5),
    status        TEXT NOT NULL CHECK (status IN ('pending','active','closed')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE fut_copy_follows (
    id             BIGSERIAL PRIMARY KEY,
    follower       TEXT NOT NULL,
    trader         TEXT NOT NULL REFERENCES fut_copy_traders(user_id),
    status         TEXT NOT NULL CHECK (status IN ('active','stopping','stopped')),
    mode           TEXT NOT NULL CHECK (mode IN ('fixed','ratio')),
    fixed_margin   NUMERIC(36,8) NOT NULL DEFAULT 0,
    ratio          NUMERIC(12,6) NOT NULL DEFAULT 1,
    max_per_trade  NUMERIC(36,8) NOT NULL DEFAULT 0,   -- 0 = no cap (margin per copied open)
    tp_ratio       NUMERIC(12,6) NOT NULL DEFAULT 0,   -- ROE, 0 = none
    sl_ratio       NUMERIC(12,6) NOT NULL DEFAULT 0,
    principal      NUMERIC(36,8) NOT NULL,             -- USDT moved in
    returned       NUMERIC(36,8) NOT NULL DEFAULT 0,   -- USDT moved back on stop
    hwm            NUMERIC(36,8) NOT NULL DEFAULT 0,   -- high-water mark of realized net already charged
    share_paid     NUMERIC(36,8) NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX fut_copy_follows_live ON fut_copy_follows (follower, trader) WHERE status <> 'stopped';
CREATE INDEX fut_copy_follows_trader ON fut_copy_follows (trader, status);

-- Trader position changes, written in the fill transaction, mirrored by the engine.
CREATE TABLE fut_copy_events (
    id         BIGSERIAL PRIMARY KEY,
    trader     TEXT NOT NULL,
    symbol     TEXT NOT NULL,
    old_qty    NUMERIC(36,8) NOT NULL,
    new_qty    NUMERIC(36,8) NOT NULL,
    price      NUMERIC(36,8) NOT NULL,
    processed  BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_copy_events_todo ON fut_copy_events (id) WHERE NOT processed;

-- Result of mirroring one event for one follow.
CREATE TABLE fut_copy_trades (
    id         BIGSERIAL PRIMARY KEY,
    follow_id  BIGINT NOT NULL,
    event_id   BIGINT,
    symbol     TEXT NOT NULL,
    action     TEXT NOT NULL,   -- open | close | stop
    side       TEXT NOT NULL,
    qty        NUMERIC(36,8) NOT NULL,
    order_id   BIGINT,
    status     TEXT NOT NULL,   -- filled | partially_filled | skipped | failed
    reason     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_copy_trades_follow ON fut_copy_trades (follow_id, id DESC);

-- Profit-share settlements, one per follow and period (YYYY-MM-DD or final).
CREATE TABLE fut_copy_settlements (
    follow_id   BIGINT NOT NULL,
    period      TEXT NOT NULL,
    trader      TEXT NOT NULL,
    realized    NUMERIC(36,8) NOT NULL,
    hwm_before  NUMERIC(36,8) NOT NULL,
    rate        NUMERIC(6,4) NOT NULL,
    share       NUMERIC(36,8) NOT NULL,
    ref         TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (follow_id, period)
);
CREATE INDEX fut_copy_settlements_trader ON fut_copy_settlements (trader, created_at);

ALTER TABLE fut_transfers ADD COLUMN kind TEXT NOT NULL DEFAULT 'transfer'; -- transfer | copy_profit_share

-- +goose Down
ALTER TABLE fut_transfers DROP COLUMN kind;
DROP TABLE fut_copy_settlements;
DROP TABLE fut_copy_trades;
DROP TABLE fut_copy_events;
DROP TABLE fut_copy_follows;
DROP TABLE fut_copy_traders;
