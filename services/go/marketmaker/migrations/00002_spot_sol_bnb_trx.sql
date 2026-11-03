-- +goose Up
-- Per-symbol MM params for the new spot pairs (rows would otherwise be
-- auto-registered with BTC-style defaults). Smaller ladders than BTC/ETH.
INSERT INTO mm_pairs (market, symbol, spread_bps, levels, level_step_bps, level_notional, size_growth, refresh_ms, requote_bps, max_position, max_skew_bps) VALUES
    ('spot', 'SOLUSDT', 12, 5, 6, 800, 1.2, 3000, 3, 400000, 25),
    ('spot', 'BNBUSDT', 12, 5, 6, 800, 1.2, 3000, 3, 400000, 25),
    ('spot', 'TRXUSDT', 20, 5, 8, 500, 1.2, 4000, 5, 200000, 30)
ON CONFLICT (market, symbol) DO NOTHING;

-- +goose Down
DELETE FROM mm_pairs WHERE market = 'spot' AND symbol IN ('SOLUSDT','BNBUSDT','TRXUSDT');
