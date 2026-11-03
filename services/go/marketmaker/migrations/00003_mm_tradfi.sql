-- +goose Up
-- MM params for TradFi spot pairs. Quotes centre on the TradFi reference
-- (Binance TradFi perp, OKX backup); market-data reports widenFactor when spot
-- deviates beyond the configured threshold and the maker multiplies spread_bps.
INSERT INTO mm_pairs (market, symbol, spread_bps, levels, level_step_bps, level_notional, size_growth, refresh_ms, requote_bps, max_position, max_skew_bps)
SELECT 'spot', s, 20, 5, 8, 500, 1.2, 4000, 5, 200000, 30
FROM unnest('{AAPLUSDT,TSLAUSDT,NVDAUSDT,MSFTUSDT,GOOGLUSDT,AMZNUSDT,METAUSDT,COINUSDT,SPYUSDT}'::text[]) s
ON CONFLICT (market, symbol) DO NOTHING;
INSERT INTO mm_pairs (market, symbol, spread_bps, levels, level_step_bps, level_notional, size_growth, refresh_ms, requote_bps, max_position, max_skew_bps) VALUES
    ('spot', 'PAXGUSDT', 15, 5, 6, 800, 1.2, 3000, 3, 300000, 25),
    ('spot', 'XAUTUSDT', 15, 5, 6, 800, 1.2, 3000, 3, 300000, 25)
ON CONFLICT (market, symbol) DO NOTHING;

-- +goose Down
DELETE FROM mm_pairs WHERE market = 'spot' AND symbol IN ('AAPLUSDT','TSLAUSDT','NVDAUSDT','MSFTUSDT','GOOGLUSDT','AMZNUSDT','METAUSDT','COINUSDT','SPYUSDT','PAXGUSDT','XAUTUSDT');
