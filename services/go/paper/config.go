package main

import (
	"github.com/linbit/platform/services/go/common"
	"strings"
	"time"
)

type Config struct {
	Port                string
	DatabaseURL         string
	InternalToken       string
	JWTSecret           string
	MarketURL           string
	AccountURL          string
	IndexMaxAge         time.Duration
	EvaluationInterval  time.Duration
	DeliveryInterval    time.Duration
	AlertCooldown       time.Duration
	AlertWriteLimit     int
	AlertWriteWindow    time.Duration
	DevMode             bool
	PaperTradingEnabled string
	PriceAlertsEnabled  string
}

func LoadConfig() Config {
	return Config{
		Port: common.Env("PORT", "8094"), DatabaseURL: common.MustEnv("DATABASE_URL"),
		InternalToken: common.MustEnv("INTERNAL_TOKEN"), JWTSecret: common.MustEnv("JWT_SECRET"),
		MarketURL: common.Env("MARKET_URL", "http://localhost:8085"), AccountURL: common.Env("ACCOUNT_INTERNAL_URL", "http://localhost:8081"),
		IndexMaxAge:         time.Duration(common.EnvInt("PAPER_INDEX_MAX_AGE_SECONDS", 10)) * time.Second,
		EvaluationInterval:  time.Duration(common.EnvInt("PRICE_ALERT_EVALUATION_MS", 2000)) * time.Millisecond,
		DeliveryInterval:    time.Duration(common.EnvInt("PRICE_ALERT_DELIVERY_MS", 1000)) * time.Millisecond,
		AlertCooldown:       time.Duration(common.EnvInt("USER_PRICE_ALERT_COOLDOWN_SECONDS", 300)) * time.Second,
		AlertWriteLimit:     common.EnvInt("PRICE_ALERT_WRITE_LIMIT", 20),
		AlertWriteWindow:    time.Duration(common.EnvInt("PRICE_ALERT_WRITE_WINDOW_SECONDS", 60)) * time.Second,
		DevMode:             common.Env("DEV_MODE", "false") == "true",
		PaperTradingEnabled: strings.TrimSpace(common.Env("PAPER_TRADING_ENABLED", "")),
		PriceAlertsEnabled:  strings.TrimSpace(common.Env("PRICE_ALERTS_ENABLED", "")),
	}
}
