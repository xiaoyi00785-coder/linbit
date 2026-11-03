-- +goose Up
-- Nine more market-page category tabs, appended after ecosystem (sort 90).
-- Only SOLUSDT is currently listed among their members; others return [].
INSERT INTO market_categories (id, name, sort, symbols) VALUES
    ('solana',   'Solana 生态', 100, '{SOLUSDT}'),
    ('gamefi',   'GameFi',      110, '{}'),
    ('depin',    'DePIN',       120, '{}'),
    ('nft',      'NFT',         130, '{}'),
    ('ton',      'TON',         140, '{}'),
    ('lsd',      'LSD',         150, '{}'),
    ('web3',     'Web3',        160, '{}'),
    ('platform', '平台币',      170, '{BNBUSDT}'),
    ('payment',  '支付',        180, '{TRXUSDT}')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM market_categories WHERE id IN
    ('solana','gamefi','depin','nft','ton','lsd','web3','platform','payment');
