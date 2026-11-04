package main

import (
	"os"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config is the marketmaker runtime configuration.
type Config struct {
	Port           string
	DatabaseURL    string
	InternalToken  string
	OrderURL       string
	FuturesURL     string
	MarketURL      string
	AssetURL       string
	Enabled        bool          // MM_ENABLED: master switch; mainnet default false, testnet default true
	UserID         string        // the market-maker account (MM_USER_ID)
	Tick           time.Duration // scheduler granularity; each pair refreshes on its own refresh_ms
	SeedAmount     string        // virtual USDT per mm_testnet_seed (testnet/mock only)
	SeedBaseUSDT   string        // USDT value per base-asset seed (MM_SEED_BASE_USDT)
	SeedBaseAssets string        // base assets allowed in a seed set (MM_SEED_BASE_ASSETS)
}

// LoadConfig reads the environment.
func LoadConfig() Config {
	return Config{
		Port:           common.Env("PORT", "8093"),
		DatabaseURL:    common.MustEnv("DATABASE_URL"),
		InternalToken:  common.Env("INTERNAL_TOKEN", ""),
		OrderURL:       common.Env("ORDER_URL", "http://localhost:8084"),
		FuturesURL:     common.Env("FUTURES_URL", "http://localhost:8089"),
		MarketURL:      common.Env("MARKET_URL", "http://localhost:8085"),
		AssetURL:       common.Env("ASSET_URL", "http://localhost:8082"),
		Enabled:        mmEnabled(),
		UserID:         common.Env("MM_USER_ID", "mm:main"),
		Tick:           time.Duration(common.EnvInt("MM_TICK_MS", 500)) * time.Millisecond,
		SeedAmount:     common.Env("MM_SEED_AMOUNT", "10000000"),
		SeedBaseUSDT:   common.Env("MM_SEED_BASE_USDT", "1000000"),
		SeedBaseAssets: common.Env("MM_SEED_BASE_ASSETS", "BTC,ETH,SOL,BNB,TRX,AAPL,TSLA,NVDA,MSFT,GOOGL,AMZN,META,COIN,SPY,PAXG,XAUT"),
	}
}

// mmEnabled reads MM_ENABLED; unset defaults to false on mainnet (owner
// decision: market maker stopped at mainnet launch) and true elsewhere.
func mmEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MM_ENABLED")))
	if v == "" {
		return !common.IsMainnet()
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
