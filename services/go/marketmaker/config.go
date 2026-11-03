package main

import (
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config is the marketmaker runtime configuration.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	OrderURL      string
	FuturesURL    string
	MarketURL     string
	AssetURL      string
	UserID        string        // the market-maker account (MM_USER_ID)
	Tick          time.Duration // scheduler granularity; each pair refreshes on its own refresh_ms
	SeedAmount    string        // virtual USDT per mm_testnet_seed (testnet/mock only)
}

// LoadConfig reads the environment.
func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8093"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		OrderURL:      common.Env("ORDER_URL", "http://localhost:8084"),
		FuturesURL:    common.Env("FUTURES_URL", "http://localhost:8089"),
		MarketURL:     common.Env("MARKET_URL", "http://localhost:8085"),
		AssetURL:      common.Env("ASSET_URL", "http://localhost:8082"),
		UserID:        common.Env("MM_USER_ID", "mm:main"),
		Tick:          time.Duration(common.EnvInt("MM_TICK_MS", 500)) * time.Millisecond,
		SeedAmount:    common.Env("MM_SEED_AMOUNT", "10000000"),
	}
}
