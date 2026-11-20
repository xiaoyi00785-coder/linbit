package main

import (
	"time"

	"github.com/linbit/platform/services/go/common"
)

type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	DevMode       bool
	AssetURL      string
	TickEvery     time.Duration // STAKING_TICK_MS (60000)
}

func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8095"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		DevMode:       common.Env("DEV_MODE", "") == "true",
		AssetURL:      common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		TickEvery:     time.Duration(common.EnvInt("STAKING_TICK_MS", 60000)) * time.Millisecond,
	}
}
