package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config holds futures settings. Every value not fixed by the business rules is
// an env-configurable default (see docs/api/futures.md).
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	AssetURL      string // asset internal URL (spot <-> futures transfers)
	MarketURL     string // market-data URL (index price = spot last price)
	DevMode       bool   // enables /internal/dev/* (index override, tick, funding)

	DefaultLeverage   int    // FUTURES_DEFAULT_LEVERAGE (20, matches the UI)
	DefaultMarginMode string // FUTURES_DEFAULT_MARGIN_MODE (cross, matches the UI)

	MarketBand      decimal.Decimal // FUTURES_MARKET_PRICE_BAND (0.05): market orders fill only within mark ±5%
	TickInterval    time.Duration   // FUTURES_TICK_MS (1000): index/mark refresh, TP/SL, liquidation scan
	MarkSampleEvery time.Duration   // FUTURES_MARK_SAMPLE_SECONDS (60): mark-price history resolution
	IndexStale      time.Duration   // FUTURES_INDEX_STALE_SECONDS (30): keep last index this long if source fails

	FundingInterval time.Duration   // FUTURES_FUNDING_HOURS (8): settles at 00/08/16 UTC
	FundingInterest decimal.Decimal // FUTURES_FUNDING_INTEREST (0.0001 per interval)
	FundingClamp    decimal.Decimal // FUTURES_FUNDING_CLAMP (0.0005)
	FundingCap      decimal.Decimal // FUTURES_FUNDING_CAP (0.0075): |rate| ceiling

	ADLEnabled    bool            // FUTURES_ADL_ENABLED (true): auto-deleverage when the insurance fund cannot cover a bankruptcy
	LiqFeeRate    decimal.Decimal // FUTURES_LIQUIDATION_FEE_RATE ("" = contract MMR): to the insurance fund
	MinTransfer   decimal.Decimal // FUTURES_MIN_TRANSFER (0.01 USDT)
	TPSLRetry     time.Duration   // FUTURES_TPSL_RETRY_SECONDS (5): retry a TP/SL that found no liquidity
	TransferRetry time.Duration   // FUTURES_TRANSFER_RETRY_SECONDS (5): outbox retry for spot<->futures
}

func decEnv(key, def string) decimal.Decimal {
	v := strings.TrimSpace(common.Env(key, def))
	d, err := decimal.NewFromString(v)
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}

// LoadConfig reads the configuration from the environment.
func LoadConfig() Config {
	mode := strings.ToLower(common.Env("FUTURES_DEFAULT_MARGIN_MODE", "cross"))
	if mode != "isolated" {
		mode = "cross"
	}
	c := Config{
		Port:              common.Env("PORT", "8089"),
		DatabaseURL:       common.Env("DATABASE_URL", "postgres://linbit:linbit@localhost:5432/linbit?sslmode=disable"),
		InternalToken:     common.Env("INTERNAL_TOKEN", ""),
		AssetURL:          common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		MarketURL:         common.Env("MARKET_URL", "http://localhost:8085"),
		DevMode:           common.Env("DEV_MODE", "false") == "true",
		DefaultLeverage:   common.EnvInt("FUTURES_DEFAULT_LEVERAGE", 20),
		DefaultMarginMode: mode,
		MarketBand:        decEnv("FUTURES_MARKET_PRICE_BAND", "0.05"),
		TickInterval:      time.Duration(common.EnvInt("FUTURES_TICK_MS", 1000)) * time.Millisecond,
		MarkSampleEvery:   time.Duration(common.EnvInt("FUTURES_MARK_SAMPLE_SECONDS", 60)) * time.Second,
		IndexStale:        time.Duration(common.EnvInt("FUTURES_INDEX_STALE_SECONDS", 30)) * time.Second,
		FundingInterval:   time.Duration(common.EnvInt("FUTURES_FUNDING_HOURS", 8)) * time.Hour,
		FundingInterest:   decEnv("FUTURES_FUNDING_INTEREST", "0.0001"),
		FundingClamp:      decEnv("FUTURES_FUNDING_CLAMP", "0.0005"),
		FundingCap:        decEnv("FUTURES_FUNDING_CAP", "0.0075"),
		MinTransfer:       decEnv("FUTURES_MIN_TRANSFER", "0.01"),
		TPSLRetry:         time.Duration(common.EnvInt("FUTURES_TPSL_RETRY_SECONDS", 5)) * time.Second,
		TransferRetry:     time.Duration(common.EnvInt("FUTURES_TRANSFER_RETRY_SECONDS", 5)) * time.Second,
		ADLEnabled:        strings.ToLower(common.Env("FUTURES_ADL_ENABLED", "true")) != "false",
	}
	if v := strings.TrimSpace(common.Env("FUTURES_LIQUIDATION_FEE_RATE", "")); v != "" {
		if d, err := decimal.NewFromString(v); err == nil && d.Sign() >= 0 {
			c.LiqFeeRate = d
		}
	} else {
		c.LiqFeeRate = decimal.NewFromInt(-1) // sentinel: use the contract MMR
	}
	if c.DefaultLeverage < 1 {
		c.DefaultLeverage = 20
	}
	return c
}
