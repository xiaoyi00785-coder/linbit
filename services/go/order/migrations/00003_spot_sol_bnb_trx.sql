-- +goose Up
-- List spot SOLUSDT, BNBUSDT, TRXUSDT (Yaeko 2026-11-03). TRX trades around
-- 0.1-0.3 USDT, so it gets 5 price decimals (tick 0.00001) and whole-unit
-- quantities; all fit the engine's 8-decimal fixed point. Engine SYMBOLS must
-- list them too (SOLUSDT:3,BNBUSDT:3,TRXUSDT:1).
INSERT INTO symbols (symbol, base, quote, price_precision, quantity_precision, min_quantity, min_notional, status) VALUES
    ('SOLUSDT', 'SOL', 'USDT', 2, 3, 0.001, 5, 'trading'),
    ('BNBUSDT', 'BNB', 'USDT', 2, 3, 0.001, 5, 'trading'),
    ('TRXUSDT', 'TRX', 'USDT', 5, 1, 1,     5, 'trading')
ON CONFLICT (symbol) DO NOTHING;

-- +goose Down
DELETE FROM symbols WHERE symbol IN ('SOLUSDT','BNBUSDT','TRXUSDT');
