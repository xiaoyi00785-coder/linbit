-- +goose Up
-- TradFi: tokenized stocks (xStocks + Ondo) and metals (Yaeko 2026-11-03).
-- One platform asset per underlying (e.g. AAPL); each issuer's token is a
-- separate on-chain network entry in tradfi_tokens (fungible 1:1 on the
-- platform ledger — see docs/tradfi.md). Reference prices come from the
-- Binance TradFi perp (ref_binance) with OKX SWAP (ref_okx) as backup.
CREATE TABLE tradfi_assets (
    asset         TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('stock','metal','commodity')),
    name          TEXT NOT NULL,
    underlying    TEXT NOT NULL,
    market        TEXT NOT NULL CHECK (market IN ('spot','futures')),
    symbol        TEXT NOT NULL UNIQUE,
    ref_binance   TEXT NOT NULL DEFAULT '',
    ref_okx       TEXT NOT NULL DEFAULT '',
    deviation_bps INT,                       -- NULL = tradfi_settings.deviation_bps
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused')),
    sort          INT  NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT NOT NULL DEFAULT ''
);
INSERT INTO tradfi_assets (asset, kind, name, underlying, market, symbol, ref_binance, ref_okx, sort) VALUES
    ('AAPL',  'stock',     'Apple',            'AAPL', 'spot',    'AAPLUSDT',  'AAPLUSDT',  'AAPL-USDT-SWAP',  10),
    ('TSLA',  'stock',     'Tesla',            'TSLA', 'spot',    'TSLAUSDT',  'TSLAUSDT',  'TSLA-USDT-SWAP',  20),
    ('NVDA',  'stock',     'NVIDIA',           'NVDA', 'spot',    'NVDAUSDT',  'NVDAUSDT',  'NVDA-USDT-SWAP',  30),
    ('MSFT',  'stock',     'Microsoft',        'MSFT', 'spot',    'MSFTUSDT',  'MSFTUSDT',  'MSFT-USDT-SWAP',  40),
    ('GOOGL', 'stock',     'Alphabet A',       'GOOGL','spot',    'GOOGLUSDT', 'GOOGLUSDT', 'GOOGL-USDT-SWAP', 50),
    ('AMZN',  'stock',     'Amazon',           'AMZN', 'spot',    'AMZNUSDT',  'AMZNUSDT',  'AMZN-USDT-SWAP',  60),
    ('META',  'stock',     'Meta Platforms',   'META', 'spot',    'METAUSDT',  'METAUSDT',  'META-USDT-SWAP',  70),
    ('COIN',  'stock',     'Coinbase',         'COIN', 'spot',    'COINUSDT',  'COINUSDT',  'COIN-USDT-SWAP',  80),
    ('SPY',   'stock',     'SPDR S&P 500 ETF', 'SPY',  'spot',    'SPYUSDT',   'SPYUSDT',   'SPY-USDT-SWAP',   90),
    ('PAXG',  'metal',     'PAX Gold',         'XAU',  'spot',    'PAXGUSDT',  'XAUUSDT',   'XAU-USDT-SWAP',  110),
    ('XAUT',  'metal',     'Tether Gold',      'XAU',  'spot',    'XAUTUSDT',  'XAUUSDT',   'XAU-USDT-SWAP',  120),
    ('XAG',   'metal',     'Silver',           'XAG',  'futures', 'XAGUSDT',   'XAGUSDT',   'XAG-USDT-SWAP',  130),
    ('WTI',   'commodity', 'WTI Crude Oil',    'CL',   'futures', 'WTIUSDT',   'CLUSDT',    'CL-USDT-SWAP',   140);

-- Issuer tokens per network. Contract addresses are configured in admin;
-- deposit/withdraw stay off until an address is set (enforced by the API).
CREATE TABLE tradfi_tokens (
    asset            TEXT NOT NULL REFERENCES tradfi_assets(asset),
    issuer           TEXT NOT NULL CHECK (issuer IN ('xstocks','ondo','paxos','tether')),
    network          TEXT NOT NULL,
    contract_address TEXT NOT NULL DEFAULT '',
    decimals         INT  NOT NULL DEFAULT 18,
    deposit_enabled  BOOLEAN NOT NULL DEFAULT FALSE,
    withdraw_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    min_withdraw     NUMERIC(36,18) NOT NULL DEFAULT 0.05,
    withdraw_fee     NUMERIC(36,18) NOT NULL DEFAULT 0.02,  -- charged in the asset itself
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (asset, issuer, network)
);
-- network = wallet network name (SOL / ERC20 / TRC20). xStocks (Backed) are
-- SPL tokens on Solana; Ondo Global Markets tokens are ERC-20 on Ethereum.
-- Rows carry no address: ops fill verified addresses in admin before enabling.
INSERT INTO tradfi_tokens (asset, issuer, network, decimals)
SELECT a, 'xstocks', 'SOL', 8 FROM unnest('{AAPL,TSLA,NVDA,MSFT,GOOGL,AMZN,META,COIN,SPY}'::text[]) a
UNION ALL
SELECT a, 'ondo', 'ERC20', 18 FROM unnest('{AAPL,TSLA,NVDA,MSFT,GOOGL,AMZN,META,COIN,SPY}'::text[]) a;
INSERT INTO tradfi_tokens (asset, issuer, network, decimals, min_withdraw, withdraw_fee) VALUES
    ('XAUT', 'tether', 'TRC20', 6,  0.005, 0.002),
    ('PAXG', 'paxos',  'ERC20', 18, 0.005, 0.002);

CREATE TABLE tradfi_settings (
    id                   INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- ISO-3166 alpha-2. US + territories + OFAC comprehensive-sanctions set.
    -- Crimea/Donetsk/Luhansk cannot be expressed as a country code.
    restricted_countries TEXT[] NOT NULL DEFAULT '{US,PR,GU,VI,AS,MP,UM,CU,IR,KP,SY}',
    deviation_bps        INT     NOT NULL DEFAULT 200,
    widen_factor         NUMERIC NOT NULL DEFAULT 3,
    alert_cooldown_sec   INT     NOT NULL DEFAULT 900,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by           TEXT NOT NULL DEFAULT ''
);
INSERT INTO tradfi_settings (id) VALUES (1);

UPDATE market_categories SET symbols = '{AAPLUSDT,TSLAUSDT,NVDAUSDT,MSFTUSDT,GOOGLUSDT,AMZNUSDT,METAUSDT,COINUSDT,SPYUSDT}', updated_at = now() WHERE id = 'stocks';
UPDATE market_categories SET symbols = '{PAXGUSDT,XAUTUSDT,XAGUSDT}', updated_at = now() WHERE id = 'metals';
UPDATE market_categories SET symbols = '{WTIUSDT}', updated_at = now() WHERE id = 'commodities';

-- +goose Down
UPDATE market_categories SET symbols = '{}' WHERE id IN ('stocks','metals','commodities');
DROP TABLE tradfi_settings;
DROP TABLE tradfi_tokens;
DROP TABLE tradfi_assets;
