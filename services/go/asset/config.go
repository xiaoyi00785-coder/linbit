package main

import (
	"github.com/linbit/platform/services/go/common"
)

// Config holds the asset service runtime configuration.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	RedisURL      string
	MarketURL     string // MARKET_URL: index prices (convert + USDT valuation)
	OrderURL      string // order service (convert hedge IOC orders)
	FuturesURL    string // FUTURES_INTERNAL_URL: futures wallet for /assets/overview
	Convert       ConvertDefaults
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8082"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		RedisURL:      common.Env("REDIS_URL", ""),
		MarketURL:     common.Env("MARKET_URL", "http://localhost:8085"),
		OrderURL:      common.Env("ORDER_URL", "http://localhost:8084"),
		FuturesURL:    common.Env("FUTURES_INTERNAL_URL", "http://localhost:8089"),
		Convert:       loadConvertDefaults(),
	}
}

// defaultAssets are seeded (at zero) when an asset account is initialised and
// always listed by GET /assets/balances (spot and funding), so every listed
// asset shows up even before the user holds any. Keep in sync with the
// convert_assets seed (00005_convert.sql) and the wallet's deposit assets.
// USDC is only removed from lending (earn); it stays a normal spot asset.
var defaultAssets = []string{"USDT", "USDC", "BTC", "ETH", "SOL", "BNB", "TRX"}
