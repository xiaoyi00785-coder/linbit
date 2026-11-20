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
	ExpiryEvery   time.Duration
	PayAsset      string
	ListingMaxDur time.Duration
	OfferMaxDur   time.Duration
}

func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8095"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		DevMode:       common.Env("DEV_MODE", "") == "true",
		AssetURL:      common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		ExpiryEvery:   time.Duration(common.EnvInt("NFT_EXPIRY_TICK_MS", 60000)) * time.Millisecond,
		PayAsset:      common.Env("NFT_PAY_ASSET", "USDT"),
		ListingMaxDur: time.Duration(common.EnvInt("NFT_LISTING_MAX_DAYS", 180)) * 24 * time.Hour,
		OfferMaxDur:   time.Duration(common.EnvInt("NFT_OFFER_MAX_DAYS", 30)) * 24 * time.Hour,
	}
}
