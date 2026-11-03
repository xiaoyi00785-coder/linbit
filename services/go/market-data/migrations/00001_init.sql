-- +goose Up
-- Persisted candlesticks (OHLCV) per trading pair and interval. open_time is the
-- bucket start in unix seconds. Candles are upserted as trades arrive, so the
-- in-progress candle is durable too and the service recovers its full in-memory
-- state on restart. quote_volume and trades are kept for richer analytics even
-- though the REST Kline schema only exposes OHLCV.
CREATE TABLE klines (
    symbol       TEXT   NOT NULL,
    interval     TEXT   NOT NULL,
    open_time    BIGINT NOT NULL,
    open         NUMERIC(36,18) NOT NULL,
    high         NUMERIC(36,18) NOT NULL,
    low          NUMERIC(36,18) NOT NULL,
    close        NUMERIC(36,18) NOT NULL,
    volume       NUMERIC(36,18) NOT NULL DEFAULT 0,
    quote_volume NUMERIC(36,18) NOT NULL DEFAULT 0,
    trades       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (symbol, interval, open_time)
);
CREATE INDEX idx_klines_symbol_interval_time ON klines (symbol, interval, open_time DESC);

-- +goose Down
DROP TABLE klines;
