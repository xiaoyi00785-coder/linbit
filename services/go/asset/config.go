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
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8082"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		RedisURL:      common.Env("REDIS_URL", ""),
	}
}

// defaultAssets are seeded (at zero) when an asset account is initialised.
var defaultAssets = []string{"USDT", "BTC", "ETH"}
