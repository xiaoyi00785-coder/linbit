package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config holds runtime settings for the options service.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	AssetURL      string
	MarketURL     string

	RiskFreeRate decimal.Decimal // OPTIONS_RISK_FREE_RATE (0.05)
	FeeRate      decimal.Decimal // OPTIONS_FEE_RATE (0.0003 per side)
	DefaultIV    decimal.Decimal // OPTIONS_DEFAULT_IV (0.60 fallback when the book has no mid)

	TickInterval       time.Duration // OPTIONS_TICK_MS (1000): mark/greeks refresh + settlement scan
	MarkSampleEvery    time.Duration // OPTIONS_MARK_SAMPLE_SECONDS (60): history down-sampling
	SettleGrace        time.Duration // OPTIONS_SETTLE_GRACE_SECONDS (60): wait for an index sample after expiry
	MaxOrderQty        decimal.Decimal
	PortfolioMarginOn  bool // OPTIONS_PORTFOLIO_MARGIN (false by default — per-position SPAN)

	DevMode bool
}

func decEnv(key, def string) decimal.Decimal {
	v := strings.TrimSpace(common.Env(key, def))
	d, err := decimal.NewFromString(v)
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:              common.Env("PORT", "8096"),
		DatabaseURL:       common.Env("DATABASE_URL", "postgres://linbit:linbit@localhost:5432/linbit?sslmode=disable"),
		InternalToken:     common.Env("INTERNAL_TOKEN", ""),
		AssetURL:          common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		MarketURL:         common.Env("MARKET_URL", "http://localhost:8085"),
		RiskFreeRate:      decEnv("OPTIONS_RISK_FREE_RATE", "0.05"),
		FeeRate:           decEnv("OPTIONS_FEE_RATE", "0.0003"),
		DefaultIV:         decEnv("OPTIONS_DEFAULT_IV", "0.60"),
		TickInterval:      time.Duration(common.EnvInt("OPTIONS_TICK_MS", 1000)) * time.Millisecond,
		MarkSampleEvery:   time.Duration(common.EnvInt("OPTIONS_MARK_SAMPLE_SECONDS", 60)) * time.Second,
		SettleGrace:       time.Duration(common.EnvInt("OPTIONS_SETTLE_GRACE_SECONDS", 60)) * time.Second,
		MaxOrderQty:       decEnv("OPTIONS_MAX_ORDER_QTY", "1000"),
		PortfolioMarginOn: strings.ToLower(common.Env("OPTIONS_PORTFOLIO_MARGIN", "false")) == "true",
		DevMode:           common.Env("DEV_MODE", "false") == "true",
	}
}
