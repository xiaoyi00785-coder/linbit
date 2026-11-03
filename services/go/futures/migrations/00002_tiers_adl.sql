-- +goose Up
-- Tiered maintenance margin (USDT notional brackets). For a position of notional
-- N in bracket k: MM = N * mmr_k - maint_amount_k (maint_amount makes MM continuous
-- across brackets). max_leverage caps the leverage allowed while the position
-- notional is within the bracket. Values are configurable via this table.
CREATE TABLE fut_risk_tiers (
    symbol        TEXT    NOT NULL REFERENCES fut_contracts(symbol),
    tier          INT     NOT NULL,
    max_notional  NUMERIC(36,8) NOT NULL,
    mmr           NUMERIC(10,6) NOT NULL,
    maint_amount  NUMERIC(36,8) NOT NULL,
    max_leverage  INT     NOT NULL,
    PRIMARY KEY (symbol, tier)
);
INSERT INTO fut_risk_tiers (symbol, tier, max_notional, mmr, maint_amount, max_leverage) VALUES
 ('BTCUSDT', 1, 50000, 0.004, 0.000, 200),
 ('BTCUSDT', 2, 500000, 0.005, 50.000, 100),
 ('BTCUSDT', 3, 2000000, 0.01, 2550.000, 50),
 ('BTCUSDT', 4, 10000000, 0.025, 32550.000, 20),
 ('BTCUSDT', 5, 50000000, 0.05, 282550.000, 10),
 ('BTCUSDT', 6, 200000000, 0.1, 2782550.000, 5),
 ('ETHUSDT', 1, 50000, 0.004, 0.000, 200),
 ('ETHUSDT', 2, 500000, 0.005, 50.000, 100),
 ('ETHUSDT', 3, 2000000, 0.01, 2550.000, 50),
 ('ETHUSDT', 4, 10000000, 0.025, 32550.000, 20),
 ('ETHUSDT', 5, 50000000, 0.05, 282550.000, 10),
 ('ETHUSDT', 6, 200000000, 0.1, 2782550.000, 5),
 ('BNBUSDT', 1, 20000, 0.004, 0.000, 100),
 ('BNBUSDT', 2, 200000, 0.01, 120.000, 50),
 ('BNBUSDT', 3, 1000000, 0.025, 3120.000, 20),
 ('BNBUSDT', 4, 5000000, 0.05, 28120.000, 10),
 ('BNBUSDT', 5, 20000000, 0.1, 278120.000, 5),
 ('SOLUSDT', 1, 20000, 0.004, 0.000, 100),
 ('SOLUSDT', 2, 200000, 0.01, 120.000, 50),
 ('SOLUSDT', 3, 1000000, 0.025, 3120.000, 20),
 ('SOLUSDT', 4, 5000000, 0.05, 28120.000, 10),
 ('SOLUSDT', 5, 20000000, 0.1, 278120.000, 5),
 ('XRPUSDT', 1, 20000, 0.004, 0.000, 100),
 ('XRPUSDT', 2, 200000, 0.01, 120.000, 50),
 ('XRPUSDT', 3, 1000000, 0.025, 3120.000, 20),
 ('XRPUSDT', 4, 5000000, 0.05, 28120.000, 10),
 ('XRPUSDT', 5, 20000000, 0.1, 278120.000, 5),
 ('DOGEUSDT', 1, 20000, 0.004, 0.000, 100),
 ('DOGEUSDT', 2, 200000, 0.01, 120.000, 50),
 ('DOGEUSDT', 3, 1000000, 0.025, 3120.000, 20),
 ('DOGEUSDT', 4, 5000000, 0.05, 28120.000, 10),
 ('DOGEUSDT', 5, 20000000, 0.1, 278120.000, 5),
 ('ARBUSDT', 1, 10000, 0.004, 0.000, 75),
 ('ARBUSDT', 2, 100000, 0.01, 60.000, 50),
 ('ARBUSDT', 3, 500000, 0.025, 1560.000, 20),
 ('ARBUSDT', 4, 2000000, 0.05, 14060.000, 10),
 ('ARBUSDT', 5, 10000000, 0.1, 114060.000, 5),
 ('TONUSDT', 1, 10000, 0.004, 0.000, 75),
 ('TONUSDT', 2, 100000, 0.01, 60.000, 50),
 ('TONUSDT', 3, 500000, 0.025, 1560.000, 20),
 ('TONUSDT', 4, 2000000, 0.05, 14060.000, 10),
 ('TONUSDT', 5, 10000000, 0.1, 114060.000, 5),
 ('LINKUSDT', 1, 10000, 0.004, 0.000, 75),
 ('LINKUSDT', 2, 100000, 0.01, 60.000, 50),
 ('LINKUSDT', 3, 500000, 0.025, 1560.000, 20),
 ('LINKUSDT', 4, 2000000, 0.05, 14060.000, 10),
 ('LINKUSDT', 5, 10000000, 0.1, 114060.000, 5),
 ('AVAXUSDT', 1, 10000, 0.004, 0.000, 75),
 ('AVAXUSDT', 2, 100000, 0.01, 60.000, 50),
 ('AVAXUSDT', 3, 500000, 0.025, 1560.000, 20),
 ('AVAXUSDT', 4, 2000000, 0.05, 14060.000, 10),
 ('AVAXUSDT', 5, 10000000, 0.1, 114060.000, 5);

-- Auto-deleveraging fills: a bankrupt position closed against an opposing user
-- position at the bankruptcy price when the insurance fund cannot cover it.
CREATE TABLE fut_adl (
    id            BIGSERIAL PRIMARY KEY,
    symbol        TEXT NOT NULL,
    liq_user      TEXT NOT NULL,
    counter_user  TEXT NOT NULL,
    qty           NUMERIC(36,8) NOT NULL,   -- signed, liquidated side
    price         NUMERIC(36,8) NOT NULL,
    counter_pnl   NUMERIC(36,8) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fut_adl_counter ON fut_adl (counter_user, id);

-- +goose Down
DROP TABLE fut_adl;
DROP TABLE fut_risk_tiers;
