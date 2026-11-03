package main

import (
	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config holds the order service runtime configuration.
//
// Trading fees are fully configurable and grouped by product. Spot maker/taker
// default to 0.1% each. Contract maker/taker default to 0.05% each and are
// charged on both open and close by position value — the contract product is not
// implemented yet, but its rates live here so the config surface is ready and
// there are no VIP tiers to special-case. Withdrawals charge no platform fee
// (only on-chain gas, handled by the wallet service), so no fee lives here.
type Config struct {
	Port               string
	DatabaseURL        string
	InternalToken      string
	AssetInternalURL   string
	EngineURL          string
	FuturesInternalURL string
	RedisURL           string

	// Spot fees (applied by this service).
	SpotMakerFeeRate decimal.Decimal
	SpotTakerFeeRate decimal.Decimal
	// Contract fees (reserved; consumed by the future perpetuals service).
	ContractMakerFeeRate decimal.Decimal
	ContractTakerFeeRate decimal.Decimal

	FeeAccount string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:                 common.Env("PORT", "8084"),
		DatabaseURL:          common.MustEnv("DATABASE_URL"),
		InternalToken:        common.Env("INTERNAL_TOKEN", ""),
		AssetInternalURL:     common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		EngineURL:            common.Env("ENGINE_URL", "http://localhost:8083"),
		FuturesInternalURL:   common.Env("FUTURES_INTERNAL_URL", ""),
		RedisURL:             common.Env("REDIS_URL", ""),
		SpotMakerFeeRate:     feeRate("SPOT_MAKER_FEE_RATE", "MAKER_FEE_RATE", "0.001"),
		SpotTakerFeeRate:     feeRate("SPOT_TAKER_FEE_RATE", "TAKER_FEE_RATE", "0.001"),
		ContractMakerFeeRate: feeRate("CONTRACT_MAKER_FEE_RATE", "", "0.0005"),
		ContractTakerFeeRate: feeRate("CONTRACT_TAKER_FEE_RATE", "", "0.0005"),
		FeeAccount:           common.Env("FEE_ACCOUNT", "platform"),
	}
}

// feeRate reads a rate from key, falling back to legacyKey (if set) then def.
func feeRate(key, legacyKey, def string) decimal.Decimal {
	fallback := def
	if legacyKey != "" {
		fallback = common.Env(legacyKey, def)
	}
	return decimal.RequireFromString(common.Env(key, fallback))
}
