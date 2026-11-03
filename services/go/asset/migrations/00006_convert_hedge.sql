-- +goose Up
-- 闪兑 risk: USDC depeg guard + automatic spot hedging of the system:convert
-- position through a dedicated hedge account (CONVERT_HEDGE_USER_ID).
ALTER TABLE convert_config
    ADD COLUMN depeg_bps                    INT NOT NULL DEFAULT 50,
    ADD COLUMN hedge_enabled                BOOLEAN,          -- NULL = env default (CONVERT_HEDGE_ENABLED)
    ADD COLUMN hedge_slippage_bps           INT NOT NULL DEFAULT 50,
    ADD COLUMN hedge_max_order_usdt         NUMERIC(36,18) NOT NULL DEFAULT 20000,
    ADD COLUMN hedge_max_hour_usdt          NUMERIC(36,18) NOT NULL DEFAULT 200000,
    ADD COLUMN hedge_unhedged_alert_usdt    NUMERIC(36,18) NOT NULL DEFAULT 5000,
    ADD COLUMN hedge_unhedged_alert_minutes INT NOT NULL DEFAULT 10;
ALTER TABLE convert_assets
    ADD COLUMN hedge_enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN hedge_min_qty NUMERIC(36,18) NOT NULL DEFAULT 0;
UPDATE convert_assets SET hedge_min_qty = CASE asset
    WHEN 'BTC' THEN 0.0002 WHEN 'ETH' THEN 0.005 WHEN 'SOL' THEN 0.1
    WHEN 'BNB' THEN 0.02 WHEN 'TRX' THEN 50 ELSE 0 END;
UPDATE convert_assets SET hedge_enabled = false WHERE asset IN ('USDT','USDC');

-- Automatic pair pauses (USDC/USDT depeg guard).
CREATE TABLE convert_pair_state (
    pair       TEXT PRIMARY KEY,           -- e.g. USDC/USDT
    paused     BOOLEAN NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',   -- depeg | price_unavailable
    price      TEXT NOT NULL DEFAULT '',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Audit trail of automatic convert risk actions (pause/resume, hedge alerts).
CREATE TABLE convert_events (
    id         BIGSERIAL PRIMARY KEY,
    kind       TEXT NOT NULL,   -- pair_paused | pair_resumed | hedge_failed | hedge_rejected | unhedged_alert
    target     TEXT NOT NULL,
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE convert_hedges (
    id              BIGSERIAL PRIMARY KEY,
    asset           TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    side            TEXT NOT NULL,          -- buy | sell
    quantity        NUMERIC(36,18) NOT NULL,
    limit_price     NUMERIC(36,18) NOT NULL,
    index_price     NUMERIC(36,18) NOT NULL,
    net_before      NUMERIC(36,18) NOT NULL,
    order_id        TEXT NOT NULL DEFAULT '',
    filled_qty      NUMERIC(36,18) NOT NULL DEFAULT 0,
    quote_amount    NUMERIC(36,18) NOT NULL DEFAULT 0, -- USDT paid (buy) / received (sell), after fees
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | filled | partial | unfilled | rejected | failed
    error           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX convert_hedges_recent ON convert_hedges (created_at DESC);
CREATE INDEX convert_hedges_asset ON convert_hedges (asset, id DESC);

-- +goose Down
DROP TABLE convert_hedges;
DROP TABLE convert_events;
DROP TABLE convert_pair_state;
ALTER TABLE convert_assets DROP COLUMN hedge_min_qty, DROP COLUMN hedge_enabled;
ALTER TABLE convert_config DROP COLUMN hedge_unhedged_alert_minutes, DROP COLUMN hedge_unhedged_alert_usdt,
    DROP COLUMN hedge_max_hour_usdt, DROP COLUMN hedge_max_order_usdt, DROP COLUMN hedge_slippage_bps,
    DROP COLUMN hedge_enabled, DROP COLUMN depeg_bps;
