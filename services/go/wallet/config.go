package main

import (
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
	// The rules file does not specify; default is "extra" (see summary).
	GasDeductMode string

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
		FeeAccount:         common.Env("WALLET_FEE_ACCOUNT", "platform:gas"),
		GasDeductMode:      gasMode(common.Env("WALLET_GAS_DEDUCT_MODE", "extra")),
		Risk: RiskThresholds{
			AutoMaxAmount:   decimalEnv("WALLET_RISK_AUTO_MAX_AMOUNT", "1000"),
			AutoMaxScore:    common.EnvInt("WALLET_RISK_AUTO_MAX_SCORE", 20),
			ReviewMinAmount: decimalEnv("WALLET_RISK_REVIEW_MIN_AMOUNT", "5000"),
			ReviewMaxScore:  common.EnvInt("WALLET_RISK_REVIEW_MAX_SCORE", 40),
			MiddleAction:    middleAction(common.Env("WALLET_RISK_MIDDLE_ACTION", "review")),
		},
		CreditBelowMinDeposit: common.Env("WALLET_CREDIT_BELOW_MIN_DEPOSIT", "true") != "false",
		Networks:              loadNetworks(),
	}
}

func gasMode(v string) string {
	if v == "amount" {
		return "amount"
	}
	return "extra"
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
