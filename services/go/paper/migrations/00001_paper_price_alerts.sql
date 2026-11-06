-- +goose Up
-- Independent virtual spot schema ONLY: no real balances/ledger/order writes.
CREATE TABLE paper_accounts (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL UNIQUE,
 generation BIGINT NOT NULL DEFAULT 1 CHECK (generation > 0),
 initial_balance NUMERIC(38,18) NOT NULL DEFAULT 10000 CHECK (initial_balance = 10000),
 resets_used_today INTEGER NOT NULL DEFAULT 0 CHECK (resets_used_today >= 0),
 reset_day DATE,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE paper_balances (
 account_id TEXT NOT NULL REFERENCES paper_accounts(id),
 generation BIGINT NOT NULL,
 asset TEXT NOT NULL,
 free NUMERIC(38,18) NOT NULL CHECK (free >= 0),
 locked NUMERIC(38,18) NOT NULL DEFAULT 0 CHECK (locked = 0),
 PRIMARY KEY (account_id,generation,asset)
);
CREATE TABLE paper_ledger_entries (
 id BIGSERIAL PRIMARY KEY,
 account_id TEXT NOT NULL REFERENCES paper_accounts(id),
 generation BIGINT NOT NULL,
 asset TEXT NOT NULL,
 delta NUMERIC(38,18) NOT NULL,
 balance_after NUMERIC(38,18) NOT NULL CHECK (balance_after >= 0),
 business_ref TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 UNIQUE (account_id,generation,business_ref,asset)
);
CREATE TABLE paper_orders (
 order_id TEXT PRIMARY KEY,
 account_id TEXT NOT NULL REFERENCES paper_accounts(id),
 user_id TEXT NOT NULL,
 generation BIGINT NOT NULL,
 client_order_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 response_json JSONB NOT NULL,
 symbol TEXT NOT NULL,
 side TEXT NOT NULL CHECK (side IN ('buy','sell')),
 order_type TEXT NOT NULL CHECK (order_type = 'market'),
 price NUMERIC(38,18),
 quantity NUMERIC(38,18) NOT NULL CHECK (quantity > 0),
 quote_order_qty NUMERIC(38,18) CHECK (quote_order_qty IS NULL OR quote_order_qty > 0),
 filled_quantity NUMERIC(38,18) NOT NULL CHECK (filled_quantity > 0 AND filled_quantity = quantity),
 status TEXT NOT NULL CHECK (status = 'filled'),
 reference_price NUMERIC(38,18) NOT NULL CHECK (reference_price > 0),
 market_observed_at TIMESTAMPTZ NOT NULL,
 fee NUMERIC(38,18) NOT NULL DEFAULT 0 CHECK (fee = 0),
 fee_asset TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 executed_at TIMESTAMPTZ NOT NULL,
 UNIQUE (user_id,client_order_id)
);
CREATE INDEX paper_orders_history ON paper_orders (user_id,generation,created_at DESC,order_id DESC);
CREATE TABLE paper_trades (
 trade_id TEXT PRIMARY KEY,
 order_id TEXT NOT NULL UNIQUE REFERENCES paper_orders(order_id),
 account_id TEXT NOT NULL REFERENCES paper_accounts(id),
 user_id TEXT NOT NULL,
 generation BIGINT NOT NULL,
 symbol TEXT NOT NULL,
 side TEXT NOT NULL CHECK (side IN ('buy','sell')),
 price NUMERIC(38,18) NOT NULL CHECK (price > 0),
 quantity NUMERIC(38,18) NOT NULL CHECK (quantity > 0),
 fee NUMERIC(38,18) NOT NULL DEFAULT 0 CHECK (fee = 0),
 fee_asset TEXT NOT NULL,
 role TEXT NOT NULL DEFAULT 'taker' CHECK (role = 'taker'),
 price_source TEXT NOT NULL DEFAULT 'INDEX' CHECK (price_source = 'INDEX'),
 market_observed_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX paper_trades_history ON paper_trades (user_id,generation,created_at DESC,trade_id DESC);
CREATE TABLE paper_reset_idempotency (
 user_id TEXT NOT NULL,
 idempotency_key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 response_json JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (user_id,idempotency_key)
);
CREATE TABLE price_alerts (
 alert_id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL,
 client_alert_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 market TEXT NOT NULL CHECK (market = 'spot'),
 symbol TEXT NOT NULL,
 condition TEXT NOT NULL CHECK (condition IN ('gte','lte')),
 threshold NUMERIC(38,18) NOT NULL CHECK (threshold > 0),
 price_source TEXT NOT NULL DEFAULT 'INDEX' CHECK (price_source = 'INDEX'),
 trigger_mode TEXT NOT NULL DEFAULT 'once' CHECK (trigger_mode = 'once'),
 channels JSONB NOT NULL DEFAULT '["in_app"]' CHECK (channels = '["in_app"]'::jsonb),
 enabled BOOLEAN NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('active','triggering','triggered','disabled','deleted')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 evaluation_epoch BIGINT NOT NULL DEFAULT 1,
 last_observed_price NUMERIC(38,18),
 last_evaluated_at TIMESTAMPTZ,
 triggered_price NUMERIC(38,18),
 triggered_at TIMESTAMPTZ,
 notification_id TEXT,
 delivery_status TEXT NOT NULL DEFAULT 'none',
 cooldown_until TIMESTAMPTZ,
 note TEXT,
 deleted_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 UNIQUE (user_id,client_alert_id)
);
CREATE INDEX price_alerts_list ON price_alerts (user_id,status,symbol,created_at DESC);
CREATE INDEX price_alerts_active_symbol ON price_alerts (symbol,alert_id) WHERE status = 'active';
CREATE TABLE price_alert_trigger_history (
 alert_id TEXT NOT NULL,
 evaluation_epoch BIGINT NOT NULL,
 symbol TEXT NOT NULL,
 threshold NUMERIC(38,18) NOT NULL,
 condition TEXT NOT NULL,
 previous_price NUMERIC(38,18),
 observed_price NUMERIC(38,18) NOT NULL,
 observed_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (alert_id,evaluation_epoch)
);
CREATE TABLE price_alert_outbox (
 alert_id TEXT NOT NULL,
 evaluation_epoch BIGINT NOT NULL,
 user_id TEXT NOT NULL,
 symbol TEXT NOT NULL,
 condition TEXT NOT NULL,
 threshold NUMERIC(38,18) NOT NULL,
 observed_price NUMERIC(38,18) NOT NULL,
 observed_at TIMESTAMPTZ NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','delivering','delivered','failed','expired')),
 notification_id TEXT,
 attempt_count INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TIMESTAMPTZ NOT NULL,
 last_error TEXT,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (alert_id,evaluation_epoch)
);
CREATE INDEX price_alert_outbox_pending ON price_alert_outbox (next_attempt_at,alert_id) WHERE state = 'pending';

-- +goose Down
DROP TABLE price_alert_outbox;
DROP TABLE price_alert_trigger_history;
DROP TABLE price_alerts;
DROP TABLE paper_reset_idempotency;
DROP TABLE paper_trades;
DROP TABLE paper_orders;
DROP TABLE paper_ledger_entries;
DROP TABLE paper_balances;
DROP TABLE paper_accounts;
