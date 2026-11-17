-- +goose Up
-- The top bracket is the largest position value allowed at the contract's highest
-- leverage. 50,000 USDT at 200x meant a margin of only 250 USDT, so a 40,000
-- USDT cost order (about 8,000,000 USDT of position value) was rejected.
-- Raise each ladder so the top leverage allows about 100,000 USDT of margin
-- (notional = margin × leverage). Later brackets stay larger. maint_amount
-- keeps maintenance margin continuous at each boundary:
-- maint_k = maint_{k-1} + max_notional_{k-1} × (mmr_k − mmr_{k-1}).

UPDATE fut_risk_tiers AS t SET
    max_notional = v.max_notional,
    mmr = v.mmr,
    maint_amount = v.maint_amount,
    max_leverage = v.max_leverage
FROM (VALUES
    ('BTCUSDT', 1, 20000000::numeric, 0.004::numeric, 0::numeric, 200),
    ('BTCUSDT', 2, 50000000, 0.005, 20000, 100),
    ('BTCUSDT', 3, 100000000, 0.01, 270000, 50),
    ('BTCUSDT', 4, 200000000, 0.025, 1770000, 20),
    ('BTCUSDT', 5, 500000000, 0.05, 6770000, 10),
    ('BTCUSDT', 6, 1000000000, 0.1, 31770000, 5),
    ('ETHUSDT', 1, 20000000, 0.004, 0, 200),
    ('ETHUSDT', 2, 50000000, 0.005, 20000, 100),
    ('ETHUSDT', 3, 100000000, 0.01, 270000, 50),
    ('ETHUSDT', 4, 200000000, 0.025, 1770000, 20),
    ('ETHUSDT', 5, 500000000, 0.05, 6770000, 10),
    ('ETHUSDT', 6, 1000000000, 0.1, 31770000, 5),
    ('BNBUSDT', 1, 10000000, 0.004, 0, 100),
    ('BNBUSDT', 2, 20000000, 0.01, 60000, 50),
    ('BNBUSDT', 3, 50000000, 0.025, 360000, 20),
    ('BNBUSDT', 4, 100000000, 0.05, 1610000, 10),
    ('BNBUSDT', 5, 200000000, 0.1, 6610000, 5),
    ('SOLUSDT', 1, 10000000, 0.004, 0, 100),
    ('SOLUSDT', 2, 20000000, 0.01, 60000, 50),
    ('SOLUSDT', 3, 50000000, 0.025, 360000, 20),
    ('SOLUSDT', 4, 100000000, 0.05, 1610000, 10),
    ('SOLUSDT', 5, 200000000, 0.1, 6610000, 5),
    ('XRPUSDT', 1, 10000000, 0.004, 0, 100),
    ('XRPUSDT', 2, 20000000, 0.01, 60000, 50),
    ('XRPUSDT', 3, 50000000, 0.025, 360000, 20),
    ('XRPUSDT', 4, 100000000, 0.05, 1610000, 10),
    ('XRPUSDT', 5, 200000000, 0.1, 6610000, 5),
    ('DOGEUSDT', 1, 10000000, 0.004, 0, 100),
    ('DOGEUSDT', 2, 20000000, 0.01, 60000, 50),
    ('DOGEUSDT', 3, 50000000, 0.025, 360000, 20),
    ('DOGEUSDT', 4, 100000000, 0.05, 1610000, 10),
    ('DOGEUSDT', 5, 200000000, 0.1, 6610000, 5),
    ('ARBUSDT', 1, 7500000, 0.004, 0, 75),
    ('ARBUSDT', 2, 15000000, 0.01, 45000, 50),
    ('ARBUSDT', 3, 40000000, 0.025, 270000, 20),
    ('ARBUSDT', 4, 80000000, 0.05, 1270000, 10),
    ('ARBUSDT', 5, 150000000, 0.1, 5270000, 5),
    ('TONUSDT', 1, 7500000, 0.004, 0, 75),
    ('TONUSDT', 2, 15000000, 0.01, 45000, 50),
    ('TONUSDT', 3, 40000000, 0.025, 270000, 20),
    ('TONUSDT', 4, 80000000, 0.05, 1270000, 10),
    ('TONUSDT', 5, 150000000, 0.1, 5270000, 5),
    ('LINKUSDT', 1, 7500000, 0.004, 0, 75),
    ('LINKUSDT', 2, 15000000, 0.01, 45000, 50),
    ('LINKUSDT', 3, 40000000, 0.025, 270000, 20),
    ('LINKUSDT', 4, 80000000, 0.05, 1270000, 10),
    ('LINKUSDT', 5, 150000000, 0.1, 5270000, 5),
    ('AVAXUSDT', 1, 7500000, 0.004, 0, 75),
    ('AVAXUSDT', 2, 15000000, 0.01, 45000, 50),
    ('AVAXUSDT', 3, 40000000, 0.025, 270000, 20),
    ('AVAXUSDT', 4, 80000000, 0.05, 1270000, 10),
    ('AVAXUSDT', 5, 150000000, 0.1, 5270000, 5),
    ('XAGUSDT', 1, 5000000, 0.01, 0, 50),
    ('XAGUSDT', 2, 15000000, 0.02, 50000, 25),
    ('XAGUSDT', 3, 40000000, 0.05, 500000, 10),
    ('XAGUSDT', 4, 100000000, 0.10, 2500000, 5),
    ('WTIUSDT', 1, 5000000, 0.01, 0, 50),
    ('WTIUSDT', 2, 15000000, 0.02, 50000, 25),
    ('WTIUSDT', 3, 40000000, 0.05, 500000, 10),
    ('WTIUSDT', 4, 100000000, 0.10, 2500000, 5)
) AS v(symbol, tier, max_notional, mmr, maint_amount, max_leverage)
WHERE t.symbol = v.symbol AND t.tier = v.tier;

-- A 40,000 USDT cost at 200x is about 125 BTC, above the old per-order max of 100.
UPDATE fut_contracts SET max_qty = CASE symbol
    WHEN 'BTCUSDT' THEN 1000
    WHEN 'ETHUSDT' THEN 20000
    WHEN 'BNBUSDT' THEN 50000
    WHEN 'SOLUSDT' THEN 500000
    WHEN 'XRPUSDT' THEN 20000000
    WHEN 'DOGEUSDT' THEN 200000000
    WHEN 'ARBUSDT' THEN 20000000
    WHEN 'TONUSDT' THEN 5000000
    WHEN 'LINKUSDT' THEN 2000000
    WHEN 'AVAXUSDT' THEN 2000000
    ELSE max_qty
END
WHERE symbol IN ('BTCUSDT','ETHUSDT','BNBUSDT','SOLUSDT','XRPUSDT','DOGEUSDT','ARBUSDT','TONUSDT','LINKUSDT','AVAXUSDT');

-- +goose Down
UPDATE fut_risk_tiers AS t SET
    max_notional = v.max_notional,
    mmr = v.mmr,
    maint_amount = v.maint_amount,
    max_leverage = v.max_leverage
FROM (VALUES
    ('BTCUSDT', 1, 50000::numeric, 0.004::numeric, 0::numeric, 200),
    ('BTCUSDT', 2, 500000, 0.005, 50, 100),
    ('BTCUSDT', 3, 2000000, 0.01, 2550, 50),
    ('BTCUSDT', 4, 10000000, 0.025, 32550, 20),
    ('BTCUSDT', 5, 50000000, 0.05, 282550, 10),
    ('BTCUSDT', 6, 200000000, 0.1, 2782550, 5),
    ('ETHUSDT', 1, 50000, 0.004, 0, 200),
    ('ETHUSDT', 2, 500000, 0.005, 50, 100),
    ('ETHUSDT', 3, 2000000, 0.01, 2550, 50),
    ('ETHUSDT', 4, 10000000, 0.025, 32550, 20),
    ('ETHUSDT', 5, 50000000, 0.05, 282550, 10),
    ('ETHUSDT', 6, 200000000, 0.1, 2782550, 5),
    ('BNBUSDT', 1, 20000, 0.004, 0, 100),
    ('BNBUSDT', 2, 200000, 0.01, 120, 50),
    ('BNBUSDT', 3, 1000000, 0.025, 3120, 20),
    ('BNBUSDT', 4, 5000000, 0.05, 28120, 10),
    ('BNBUSDT', 5, 20000000, 0.1, 278120, 5),
    ('SOLUSDT', 1, 20000, 0.004, 0, 100),
    ('SOLUSDT', 2, 200000, 0.01, 120, 50),
    ('SOLUSDT', 3, 1000000, 0.025, 3120, 20),
    ('SOLUSDT', 4, 5000000, 0.05, 28120, 10),
    ('SOLUSDT', 5, 20000000, 0.1, 278120, 5),
    ('XRPUSDT', 1, 20000, 0.004, 0, 100),
    ('XRPUSDT', 2, 200000, 0.01, 120, 50),
    ('XRPUSDT', 3, 1000000, 0.025, 3120, 20),
    ('XRPUSDT', 4, 5000000, 0.05, 28120, 10),
    ('XRPUSDT', 5, 20000000, 0.1, 278120, 5),
    ('DOGEUSDT', 1, 20000, 0.004, 0, 100),
    ('DOGEUSDT', 2, 200000, 0.01, 120, 50),
    ('DOGEUSDT', 3, 1000000, 0.025, 3120, 20),
    ('DOGEUSDT', 4, 5000000, 0.05, 28120, 10),
    ('DOGEUSDT', 5, 20000000, 0.1, 278120, 5),
    ('ARBUSDT', 1, 10000, 0.004, 0, 75),
    ('ARBUSDT', 2, 100000, 0.01, 60, 50),
    ('ARBUSDT', 3, 500000, 0.025, 1560, 20),
    ('ARBUSDT', 4, 2000000, 0.05, 14060, 10),
    ('ARBUSDT', 5, 10000000, 0.1, 114060, 5),
    ('TONUSDT', 1, 10000, 0.004, 0, 75),
    ('TONUSDT', 2, 100000, 0.01, 60, 50),
    ('TONUSDT', 3, 500000, 0.025, 1560, 20),
    ('TONUSDT', 4, 2000000, 0.05, 14060, 10),
    ('TONUSDT', 5, 10000000, 0.1, 114060, 5),
    ('LINKUSDT', 1, 10000, 0.004, 0, 75),
    ('LINKUSDT', 2, 100000, 0.01, 60, 50),
    ('LINKUSDT', 3, 500000, 0.025, 1560, 20),
    ('LINKUSDT', 4, 2000000, 0.05, 14060, 10),
    ('LINKUSDT', 5, 10000000, 0.1, 114060, 5),
    ('AVAXUSDT', 1, 10000, 0.004, 0, 75),
    ('AVAXUSDT', 2, 100000, 0.01, 60, 50),
    ('AVAXUSDT', 3, 500000, 0.025, 1560, 20),
    ('AVAXUSDT', 4, 2000000, 0.05, 14060, 10),
    ('AVAXUSDT', 5, 10000000, 0.1, 114060, 5),
    ('XAGUSDT', 1, 50000, 0.01, 0, 50),
    ('XAGUSDT', 2, 250000, 0.02, 500, 25),
    ('XAGUSDT', 3, 1000000, 0.05, 8000, 10),
    ('XAGUSDT', 4, 5000000, 0.10, 58000, 5),
    ('WTIUSDT', 1, 50000, 0.01, 0, 50),
    ('WTIUSDT', 2, 250000, 0.02, 500, 25),
    ('WTIUSDT', 3, 1000000, 0.05, 8000, 10),
    ('WTIUSDT', 4, 5000000, 0.10, 58000, 5)
) AS v(symbol, tier, max_notional, mmr, maint_amount, max_leverage)
WHERE t.symbol = v.symbol AND t.tier = v.tier;

UPDATE fut_contracts SET max_qty = CASE symbol
    WHEN 'BTCUSDT' THEN 100
    WHEN 'ETHUSDT' THEN 2000
    WHEN 'BNBUSDT' THEN 5000
    WHEN 'SOLUSDT' THEN 50000
    WHEN 'XRPUSDT' THEN 2000000
    WHEN 'DOGEUSDT' THEN 20000000
    WHEN 'ARBUSDT' THEN 2000000
    WHEN 'TONUSDT' THEN 500000
    WHEN 'LINKUSDT' THEN 200000
    WHEN 'AVAXUSDT' THEN 200000
    ELSE max_qty
END
WHERE symbol IN ('BTCUSDT','ETHUSDT','BNBUSDT','SOLUSDT','XRPUSDT','DOGEUSDT','ARBUSDT','TONUSDT','LINKUSDT','AVAXUSDT');
