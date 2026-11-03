package main

import (
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config holds the account service runtime configuration, sourced from env.
type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	AssetInternalURL string
	InternalToken    string
	RedisURL         string
	// DevMode exposes the internal "last verification code" endpoint and records
	// plaintext codes in memory. Never enable in production.
	DevMode bool
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:             common.Env("PORT", "8081"),
		DatabaseURL:      common.MustEnv("DATABASE_URL"),
		JWTSecret:        common.MustEnv("JWT_SECRET"),
		AccessTTL:        time.Duration(common.EnvInt("ACCESS_TTL_SECONDS", 3600)) * time.Second,
		RefreshTTL:       time.Duration(common.EnvInt("REFRESH_TTL_SECONDS", 7*24*3600)) * time.Second,
		AssetInternalURL: common.Env("ASSET_INTERNAL_URL", ""),
		InternalToken:    common.Env("INTERNAL_TOKEN", ""),
		RedisURL:         common.Env("REDIS_URL", ""),
		DevMode:          common.Env("DEV_MODE", "") == "true",
	}
}
