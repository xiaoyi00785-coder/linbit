package main

// 现货订单运行配置。现货 maker/taker 默认各 0.1%。合约 maker/taker 默认各 0.05%，按仓位价值开平都收，
// 合约产品本身不在本服务实现，费率放在这里只为配置面齐全，没有 VIP 档。提币不收平台费（只有链上 gas，在钱包服务），这里不放提币费。

import (
	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config 的合约费率只是配置面，本服务不按它结算合约。现货费率才用于本服务的成交。
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

// LoadConfig 从环境变量读配置。缺省端口 8084。
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

// feeRate 先读 key，没有再读 legacyKey，最后用 def。值必须是合法十进制，解析失败会 panic，不会当成 0。
func feeRate(key, legacyKey, def string) decimal.Decimal {
	fallback := def
	if legacyKey != "" {
		fallback = common.Env(legacyKey, def)
	}
	return decimal.RequireFromString(common.Env(key, fallback))
}
