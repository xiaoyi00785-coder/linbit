package main

import "github.com/linbit/platform/services/go/common"

// Config holds the gateway runtime configuration.
type Config struct {
	Port       string
	JWTSecret  string
	AccountURL string
	AssetURL   string
	WalletURL  string
	OrderURL   string
	MarketURL  string
	KycURL     string
	FuturesURL string
	RedisURL   string
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
		RedisURL:       common.Env("REDIS_URL", ""),
		AllowedOrigins: splitOrigins(common.Env("CORS_ALLOWED_ORIGINS", "")),
	}
}
