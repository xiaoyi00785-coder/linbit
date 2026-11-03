package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config holds the market-data service runtime configuration.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	// EngineURL is used only to bootstrap a full depth snapshot on startup
	// (restart recovery); live depth comes from the event stream.
	EngineURL string
	RedisURL  string
	// Symbols optionally overrides the tracked trading pairs; when empty the
	// list is read from the shared `symbols` table.
	Symbols []string
	// External index feed (feed.go).
	IndexFeed      []string // ordered sources: binance, okx; empty = off (local last price)
	IndexPoll      time.Duration
	IndexStale     time.Duration
	BinanceURL     string
	OKXURL         string
	BinanceFAPIURL string // Binance USDⓈ-M futures (TradFi perps) for tokenized-stock/metal reference prices
	DevMode        bool
	// EarnURL serves the live APR for home placements tied to an earn product.
	EarnURL string
	// FiatRatesURLs are tried in order for GET /market/fiat-rates (USD base).
	FiatRatesURLs []string
	// Chat: KYC level lookup (account) and futures rooms (futures contracts).
	AccountURL string
	FuturesURL string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:           common.Env("PORT", "8085"),
		DatabaseURL:    common.MustEnv("DATABASE_URL"),
		InternalToken:  common.Env("INTERNAL_TOKEN", ""),
		EngineURL:      common.Env("ENGINE_URL", "http://localhost:8083"),
		RedisURL:       common.Env("REDIS_URL", ""),
		Symbols:        splitCSV(common.Env("SYMBOLS", "")),
		IndexFeed:      parseFeed(common.Env("INDEX_FEED", "binance,okx")),
		IndexPoll:      time.Duration(common.EnvInt("INDEX_POLL_MS", 2000)) * time.Millisecond,
		IndexStale:     time.Duration(common.EnvInt("INDEX_STALE_SECONDS", 10)) * time.Second,
		BinanceURL:     common.Env("BINANCE_API_URL", "https://data-api.binance.vision"),
		OKXURL:         common.Env("OKX_API_URL", "https://www.okx.com"),
		BinanceFAPIURL: common.Env("BINANCE_FAPI_URL", "https://fapi.binance.com"),
		DevMode:        common.Env("DEV_MODE", "") == "true",
		EarnURL:        common.Env("EARN_URL", "http://localhost:8092"),
		AccountURL:     common.Env("ACCOUNT_URL", "http://localhost:8081"),
		FuturesURL:     common.Env("FUTURES_URL", "http://localhost:8089"),
		FiatRatesURLs:  splitCSV(common.Env("FIAT_RATES_URLS", "https://open.er-api.com/v6/latest/USD,https://api.frankfurter.app/latest?from=USD")),
	}
}

// splitCSV splits a comma list into trimmed, non-empty entries.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseFeed parses INDEX_FEED ("binance,okx" | "off").
func parseFeed(v string) []string {
	var out []string
	for _, n := range splitCSV(strings.ToLower(v)) {
		if n == "off" || n == "none" {
			return nil
		}
		if n == "binance" || n == "okx" {
			out = append(out, n)
		}
	}
	return out
}
