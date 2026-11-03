-- +goose Up
-- TradFi USDT perps (Yaeko 2026-11-03): silver and WTI crude. Index = Binance
-- TradFi perp (XAGUSDT / CLUSDT) with OKX SWAP (XAG-USDT-SWAP / CL-USDT-SWAP)
-- as backup, served by market-data /market/index. Trading gated by the
-- `metals` feature flag + region check at the gateway. Lower leverage than
-- crypto (50x), MMR 1%.
INSERT INTO fut_contracts (symbol, base, max_leverage, mmr, tick_size, min_qty, max_qty, maker_fee, taker_fee, sort) VALUES
 ('XAGUSDT', 'XAG', 50, 0.01, 0.001, 0.1, 100000, 0.0005, 0.0005, 11),
 ('WTIUSDT', 'WTI', 50, 0.01, 0.01,  0.1, 100000, 0.0005, 0.0005, 12)
ON CONFLICT (symbol) DO NOTHING;
-- maint_amount_k = maint_amount_{k-1} + max_notional_{k-1} * (mmr_k - mmr_{k-1})
INSERT INTO fut_risk_tiers (symbol, tier, max_notional, mmr, maint_amount, max_leverage)
SELECT s, t.tier, t.max_notional, t.mmr, t.maint, t.lev
FROM unnest('{XAGUSDT,WTIUSDT}'::text[]) s,
     (VALUES (1, 50000, 0.01, 0, 50),
             (2, 250000, 0.02, 500, 25),
             (3, 1000000, 0.05, 8000, 10),
             (4, 5000000, 0.10, 58000, 5)) AS t(tier, max_notional, mmr, maint, lev)
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM fut_risk_tiers WHERE symbol IN ('XAGUSDT','WTIUSDT');
DELETE FROM fut_contracts WHERE symbol IN ('XAGUSDT','WTIUSDT');
