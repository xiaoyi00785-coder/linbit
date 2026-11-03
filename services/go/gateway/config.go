package main

import "github.com/linbit/platform/services/go/common"

// Config holds the gateway runtime configuration.
type Config struct {
	Port       string
	JWTSecret  string
	AccountURL string
	AssetURL   string
	RedisURL   string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:       common.Env("PORT", "8080"),
		JWTSecret:  common.MustEnv("JWT_SECRET"),
		AccountURL: common.Env("ACCOUNT_URL", "http://localhost:8081"),
		AssetURL:   common.Env("ASSET_URL", "http://localhost:8082"),
		RedisURL:   common.Env("REDIS_URL", ""),
	}
}
