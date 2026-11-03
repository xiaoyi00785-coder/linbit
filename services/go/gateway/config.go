package main

import "github.com/linbit/platform/services/go/common"

// Config holds the gateway runtime configuration.
type Config struct {
	Port        string
	JWTSecret   string
	AccountURL  string
	AssetURL    string
	WalletURL   string
	OrderURL    string
	MarketURL   string
	KycURL      string
	FuturesURL  string
	StrategyURL string
	GrowthURL   string
	EarnURL     string
	RedisURL    string
	// InternalToken authenticates gateway -> market-data/kyc internal lookups (TradFi gate).
	InternalToken string
	// AllowedOrigins is the CORS allow-list (empty = dev default: localhost/127.0.0.1).
	AllowedOrigins []string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:           common.Env("PORT", "8080"),
		JWTSecret:      common.MustEnv("JWT_SECRET"),
		AccountURL:     common.Env("ACCOUNT_URL", "http://localhost:8081"),
		AssetURL:       common.Env("ASSET_URL", "http://localhost:8082"),
		WalletURL:      common.Env("WALLET_URL", "http://localhost:8086"),
		OrderURL:       common.Env("ORDER_URL", "http://localhost:8084"),
		MarketURL:      common.Env("MARKET_URL", "http://localhost:8085"),
		KycURL:         common.Env("KYC_URL", "http://localhost:8087"),
		FuturesURL:     common.Env("FUTURES_URL", "http://localhost:8089"),
		StrategyURL:    common.Env("STRATEGY_URL", "http://localhost:8090"),
		GrowthURL:      common.Env("GROWTH_URL", "http://localhost:8091"),
		EarnURL:        common.Env("EARN_URL", "http://localhost:8092"),
		RedisURL:       common.Env("REDIS_URL", ""),
		InternalToken:  common.Env("INTERNAL_TOKEN", ""),
		AllowedOrigins: splitOrigins(common.Env("CORS_ALLOWED_ORIGINS", "")),
	}
}
