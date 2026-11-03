-- +goose Up
-- TradFi spot pairs (Yaeko 2026-11-03): 9 tokenized stocks + PAXG/XAUT.
-- Trading is gated by the tokenizedStocks / metals feature flags and region
-- checks at the gateway; engine SYMBOLS must list them too.
INSERT INTO symbols (symbol, base, quote, price_precision, quantity_precision, min_quantity, min_notional, status) VALUES
    ('AAPLUSDT',  'AAPL',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('TSLAUSDT',  'TSLA',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('NVDAUSDT',  'NVDA',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('MSFTUSDT',  'MSFT',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('GOOGLUSDT', 'GOOGL', 'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('AMZNUSDT',  'AMZN',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('METAUSDT',  'META',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('COINUSDT',  'COIN',  'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('SPYUSDT',   'SPY',   'USDT', 2, 4, 0.0001, 5, 'trading'),
    ('PAXGUSDT',  'PAXG',  'USDT', 2, 5, 0.00001, 5, 'trading'),
    ('XAUTUSDT',  'XAUT',  'USDT', 2, 5, 0.00001, 5, 'trading')
ON CONFLICT (symbol) DO NOTHING;

-- +goose Down
DELETE FROM symbols WHERE symbol IN ('AAPLUSDT','TSLAUSDT','NVDAUSDT','MSFTUSDT','GOOGLUSDT','AMZNUSDT','METAUSDT','COINUSDT','SPYUSDT','PAXGUSDT','XAUTUSDT');
