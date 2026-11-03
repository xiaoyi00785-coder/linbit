package main

import (
	"encoding/json"
	"log"

	"github.com/linbit/platform/services/go/common"
)

// Config holds the asset service runtime configuration.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	RedisURL      string

	// WithdrawNetworks maps an asset to its available withdrawal networks and
	// per-network on-chain gas estimates, surfaced by GET /wallet/withdraw/networks.
	WithdrawNetworks map[string][]WithdrawNetwork
}

// WithdrawNetwork is one withdrawable network for an asset. Amounts are decimal
// strings (never floats). gasFeeEstimate is the only withdrawal cost — there is
// no platform withdrawal fee.
type WithdrawNetwork struct {
	Network                 string `json:"network"`
	MinWithdraw             string `json:"minWithdraw,omitempty"`
	GasFeeEstimate          string `json:"gasFeeEstimate,omitempty"`
	GasFeeAsset             string `json:"gasFeeAsset,omitempty"`
	EstimatedArrivalMinutes *int   `json:"estimatedArrivalMinutes,omitempty"`
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:             common.Env("PORT", "8082"),
		DatabaseURL:      common.MustEnv("DATABASE_URL"),
		InternalToken:    common.Env("INTERNAL_TOKEN", ""),
		RedisURL:         common.Env("REDIS_URL", ""),
		WithdrawNetworks: loadWithdrawNetworks(),
	}
}

// loadWithdrawNetworks returns the withdrawal-network config. It uses built-in
// defaults unless WITHDRAW_NETWORKS_JSON holds a JSON object of
// {asset: [WithdrawNetwork, ...]} which then fully replaces the defaults.
func loadWithdrawNetworks() map[string][]WithdrawNetwork {
	if raw := common.Env("WITHDRAW_NETWORKS_JSON", ""); raw != "" {
		var m map[string][]WithdrawNetwork
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			log.Printf("config: invalid WITHDRAW_NETWORKS_JSON (%v); using defaults", err)
		} else {
			return m
		}
	}
	return defaultWithdrawNetworks()
}

func intptr(n int) *int { return &n }

// defaultWithdrawNetworks matches the openapi examples; override via
// WITHDRAW_NETWORKS_JSON for real deployments.
func defaultWithdrawNetworks() map[string][]WithdrawNetwork {
	return map[string][]WithdrawNetwork{
		"USDT": {
			{Network: "TRC20", MinWithdraw: "10.00000000", GasFeeEstimate: "1.00000000", GasFeeAsset: "USDT", EstimatedArrivalMinutes: intptr(5)},
			{Network: "ERC20", MinWithdraw: "20.00000000", GasFeeEstimate: "6.50000000", GasFeeAsset: "USDT", EstimatedArrivalMinutes: intptr(10)},
		},
		"BTC": {
			{Network: "BTC", MinWithdraw: "0.00050000", GasFeeEstimate: "0.00010000", GasFeeAsset: "BTC", EstimatedArrivalMinutes: intptr(30)},
		},
		"ETH": {
			{Network: "ERC20", MinWithdraw: "0.01000000", GasFeeEstimate: "0.00200000", GasFeeAsset: "ETH", EstimatedArrivalMinutes: intptr(10)},
		},
	}
}

// defaultAssets are seeded (at zero) when an asset account is initialised.
var defaultAssets = []string{"USDT", "BTC", "ETH"}
