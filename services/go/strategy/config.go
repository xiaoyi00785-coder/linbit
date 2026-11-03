package main

import (
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config holds the strategy (spot grid) service configuration. Every product
// parameter not fixed by the business rules is a configurable default here.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	OrderURL      string // order service (places/cancels via /spot/orders, reads via /internal)
	MarketURL     string // market-data (ticker lastPrice drives start price and SL/TP)
	DevMode       bool

	Poll            time.Duration   // GRID_POLL_MS: fill/price polling interval
	MaxBotsPerUser  int             // GRID_MAX_BOTS_PER_USER: concurrently running bots
	MinGrids        int             // GRID_MIN_GRIDS
	MaxGrids        int             // GRID_MAX_GRIDS
	MinInvestment   decimal.Decimal // GRID_MIN_INVESTMENT (quote asset)
	FeeRate         decimal.Decimal // GRID_FEE_RATE: sizing/profit estimate only (real fees come from fills)
	BuyBuffer       decimal.Decimal // GRID_BUY_BUFFER: slippage head-room on the initial market buy
	MaxRangeRatio   decimal.Decimal // GRID_MAX_RANGE_RATIO: upper/lower cap
	SnapshotEvery   time.Duration   // GRID_SNAPSHOT_SECONDS: equity curve sampling
	SquareMinRun    time.Duration   // GRID_SQUARE_MIN_RUN_SECONDS: min runtime to appear in the strategy square
	DefaultSellBase bool            // GRID_SELL_ON_STOP: default for "sell base when stopping"
}

func dec(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(common.Env(key, def))
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}

func LoadConfig() Config {
	return Config{
		Port:            common.Env("PORT", "8090"),
		DatabaseURL:     common.MustEnv("DATABASE_URL"),
		InternalToken:   common.Env("INTERNAL_TOKEN", ""),
		OrderURL:        common.Env("ORDER_URL", "http://localhost:8084"),
		MarketURL:       common.Env("MARKET_URL", "http://localhost:8085"),
		DevMode:         common.Env("DEV_MODE", "") == "true",
		Poll:            time.Duration(common.EnvInt("GRID_POLL_MS", 1000)) * time.Millisecond,
		MaxBotsPerUser:  common.EnvInt("GRID_MAX_BOTS_PER_USER", 10),
		MinGrids:        common.EnvInt("GRID_MIN_GRIDS", 2),
		MaxGrids:        common.EnvInt("GRID_MAX_GRIDS", 200),
		MinInvestment:   dec("GRID_MIN_INVESTMENT", "10"),
		FeeRate:         dec("GRID_FEE_RATE", "0.001"),
		BuyBuffer:       dec("GRID_BUY_BUFFER", "0.003"),
		MaxRangeRatio:   dec("GRID_MAX_RANGE_RATIO", "100"),
		SnapshotEvery:   time.Duration(common.EnvInt("GRID_SNAPSHOT_SECONDS", 60)) * time.Second,
		SquareMinRun:    time.Duration(common.EnvInt("GRID_SQUARE_MIN_RUN_SECONDS", 3600)) * time.Second,
		DefaultSellBase: common.Env("GRID_SELL_ON_STOP", "true") == "true",
	}
}
