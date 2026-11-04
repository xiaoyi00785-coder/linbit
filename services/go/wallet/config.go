package main

import (
	"encoding/json"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config holds the wallet service runtime configuration, sourced from env.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	RedisURL      string
	// DevMode exposes the dev-only mock-chain endpoints (inject deposit, advance
	// blocks, trigger scan/process). Never enable in production.
	DevMode bool

	// AccountInternalURL / AssetInternalURL are the service-to-service base URLs
	// used for 2FA/whitelist checks and double-entry bookkeeping respectively.
	AccountInternalURL string
	AssetInternalURL   string

	// FeeAccount is the ledger account that the withdrawal's on-chain gas is
	// booked into on completion (chain cost / platform account).
	FeeAccount string

	// GasDeductMode decides where the on-chain gas comes from:
	//   "extra"  — charged on top of the amount (frozen from the user's balance).
	//   "amount" — deducted from the withdrawal amount (the user receives amount-gas).
	// Confirmed product rule: default "amount" — the user receives amount-gas,
	// only the amount is frozen, and the amount must exceed gas.
	GasDeductMode string

	// MarketURL is the market-data service used for real-time USDT prices when
	// converting non-stablecoin withdrawals for the KYC limit.
	MarketURL string

	// Risk-based release thresholds (amounts in USD-equivalent / the asset's own
	// units for non-USDT assets). See risk.go for the decision function.
	Risk RiskThresholds

	// CreditBelowMinDeposit decides whether deposits below a network's MinDeposit
	// are still credited. The rules file only says sub-minimum deposits do not
	// satisfy withdrawal conditions (not that they are dropped), so the default is
	// true — credit anyway (see summary).
	CreditBelowMinDeposit bool

	// Networks is the per-asset network registry (confirmations, min/max, gas).
	Networks NetworkRegistry

	// KYC holds the KYC-tiered daily withdrawal limits. It is populated by
	// LoadConfig; a zero value (as in unit tests that build Config by hand) leaves
	// the limit check inert.
	KYC KYCLimitConfig
}

// KYCLimitConfig parameterises the per-KYC-level rolling-24h withdrawal limits,
// expressed in USDT-equivalent. Non-USDT amounts are converted via Prices; assets
// without a configured price are not counted toward the limit.
type KYCLimitConfig struct {
	// DailyUSDT maps a KYC level (0, 1, 2) to its rolling-24h withdrawal cap in
	// USDT-equivalent. A non-positive cap means "unlimited" for that level.
	DailyUSDT map[int]decimal.Decimal
	// MinLevel is the lowest KYC level allowed to withdraw at all (default 1:
	// unverified users get KYC_REQUIRED). 0 disables the gate; level 0 then
	// falls back to its DailyUSDT cap.
	MinLevel int
	// Prices maps an asset to its USDT price for limit conversion (USDT = 1).
	Prices map[string]decimal.Decimal
}

// limit returns the daily USDT cap for a level (and whether the feature is on),
// clamping levels above the highest configured tier down to it.
func (k KYCLimitConfig) limit(level int) (decimal.Decimal, bool) {
	if len(k.DailyUSDT) == 0 {
		return decimal.Zero, false
	}
	if v, ok := k.DailyUSDT[level]; ok {
		return v, true
	}
	best, bestLvl, found := decimal.Zero, -1, false
	for lvl, v := range k.DailyUSDT {
		if lvl <= level && lvl > bestLvl {
			best, bestLvl, found = v, lvl, true
		}
	}
	return best, found
}

// price returns an asset's USDT price and whether it is configured.
func (k KYCLimitConfig) price(asset string) (decimal.Decimal, bool) {
	p, ok := k.Prices[asset]
	return p, ok
}

// RiskThresholds parameterises the auto-release / manual-review decision.
type RiskThresholds struct {
	AutoMaxAmount   decimal.Decimal // ≤ this (and score ≤ AutoMaxScore) auto-releases
	AutoMaxScore    int
	ReviewMinAmount decimal.Decimal // ≥ this (or score > ReviewMaxScore) forces review
	ReviewMaxScore  int
	// MiddleAction is applied to the in-between band the rules file leaves blank:
	// "review" (default) or "auto".
	MiddleAction string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:               common.Env("PORT", "8086"),
		DatabaseURL:        common.MustEnv("DATABASE_URL"),
		InternalToken:      common.Env("INTERNAL_TOKEN", ""),
		RedisURL:           common.Env("REDIS_URL", ""),
		DevMode:            common.Env("DEV_MODE", "") == "true",
		AccountInternalURL: common.Env("ACCOUNT_INTERNAL_URL", ""),
		AssetInternalURL:   common.Env("ASSET_INTERNAL_URL", ""),
		MarketURL:          common.Env("MARKET_URL", "http://localhost:8085"),
		FeeAccount:         common.Env("WALLET_FEE_ACCOUNT", "platform:gas"),
		GasDeductMode:      gasMode(common.Env("WALLET_GAS_DEDUCT_MODE", "amount")),
		Risk: RiskThresholds{
			AutoMaxAmount:   decimalEnv("WALLET_RISK_AUTO_MAX_AMOUNT", "1000"),
			AutoMaxScore:    common.EnvInt("WALLET_RISK_AUTO_MAX_SCORE", 20),
			ReviewMinAmount: decimalEnv("WALLET_RISK_REVIEW_MIN_AMOUNT", map[bool]string{true: "1000", false: "5000"}[common.IsMainnet()]), /* mainnet: >1000 USDT-equivalent always manual review */
			ReviewMaxScore:  common.EnvInt("WALLET_RISK_REVIEW_MAX_SCORE", 40),
			MiddleAction:    middleAction(common.Env("WALLET_RISK_MIDDLE_ACTION", "review")),
		},
		CreditBelowMinDeposit: common.Env("WALLET_CREDIT_BELOW_MIN_DEPOSIT", "true") != "false",
		Networks:              loadNetworks(),
		KYC:                   loadKYCLimits(),
	}
}

// loadKYCLimits builds the KYC-tiered daily withdrawal limit config from env.
// Confirmed product rule: users below level 1 (no KYC) cannot withdraw at all
// (KYC_REQUIRED); level 1 = 50000, level 2 = 200000 USDT/24h.
// Non-stablecoins are converted at market-data's real-time {ASSET}USDT last
// price; if no price is available the withdrawal is rejected (PRICE_UNAVAILABLE).
// WALLET_KYC_PRICES_JSON pins fixed prices (default {"USDT":"1","USDC":"1"}).
func loadKYCLimits() KYCLimitConfig {
	return KYCLimitConfig{
		MinLevel: common.EnvInt("WALLET_KYC_MIN_LEVEL", 1),
		DailyUSDT: map[int]decimal.Decimal{
			0: decimalEnv("WALLET_KYC_LEVEL0_DAILY_USDT", "1000"),
			1: decimalEnv("WALLET_KYC_LEVEL1_DAILY_USDT", "50000"),
			2: decimalEnv("WALLET_KYC_LEVEL2_DAILY_USDT", "200000"),
		},
		Prices: loadKYCPrices(),
	}
}

func loadKYCPrices() map[string]decimal.Decimal {
	prices := map[string]decimal.Decimal{"USDT": decimal.RequireFromString("1"), "USDC": decimal.RequireFromString("1")}
	raw := common.Env("WALLET_KYC_PRICES_JSON", "")
	if raw == "" {
		return prices
	}
	var override map[string]string
	if err := json.Unmarshal([]byte(raw), &override); err != nil {
		return prices
	}
	for asset, p := range override {
		if d, err := decimal.NewFromString(p); err == nil {
			prices[strings.ToUpper(asset)] = d
		}
	}
	return prices
}

func gasMode(v string) string {
	if v == "extra" {
		return "extra"
	}
	return "amount"
}

func middleAction(v string) string {
	if v == "auto" {
		return "auto"
	}
	return "review"
}

func decimalEnv(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(common.Env(key, def))
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}
