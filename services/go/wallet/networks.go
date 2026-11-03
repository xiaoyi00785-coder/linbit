package main

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Network describes one (asset, network) pair: how many confirmations a deposit
// needs, the min/max amounts, and the on-chain gas estimate. All money fields
// are exact decimals (never floats).
type Network struct {
	Asset                   string
	Network                 string
	Confirmations           int             // deposit & withdrawal confirmations
	MinDeposit              decimal.Decimal // informational; below this is handled per CreditBelowMinDeposit
	MinWithdraw             decimal.Decimal
	MaxWithdraw             decimal.Decimal // zero = no maximum
	GasFeeEstimate          decimal.Decimal // the only withdrawal cost (no platform fee)
	GasFeeAsset             string          // kept equal to Asset in all built-in networks
	EstimatedArrivalMinutes int
	AddressPrefix           string // prefix used by the mock chain's address generator
}

// NetworkRegistry indexes networks by asset and by (asset, network).
type NetworkRegistry struct {
	byAsset map[string][]Network
	byPair  map[string]Network
}

// Lookup returns the Network for an (asset, network) pair.
func (r NetworkRegistry) Lookup(asset, network string) (Network, bool) {
	n, ok := r.byPair[pairKey(asset, network)]
	return n, ok
}

// ForAsset returns all networks configured for an asset (nil if none).
func (r NetworkRegistry) ForAsset(asset string) []Network {
	return r.byAsset[strings.ToUpper(asset)]
}

func pairKey(asset, network string) string {
	return strings.ToUpper(asset) + "|" + strings.ToUpper(network)
}

func newRegistry(nets []Network) NetworkRegistry {
	r := NetworkRegistry{byAsset: map[string][]Network{}, byPair: map[string]Network{}}
	for _, n := range nets {
		n.Asset = strings.ToUpper(n.Asset)
		n.Network = strings.ToUpper(n.Network)
		if n.GasFeeAsset == "" {
			n.GasFeeAsset = n.Asset
		}
		r.byAsset[n.Asset] = append(r.byAsset[n.Asset], n)
		r.byPair[pairKey(n.Asset, n.Network)] = n
	}
	return r
}

// loadNetworks returns the network registry. A WALLET_NETWORKS_JSON env var (a
// JSON array of networkJSON objects) fully replaces the built-in defaults.
func loadNetworks() NetworkRegistry {
	if raw := common.Env("WALLET_NETWORKS_JSON", ""); raw != "" {
		var items []networkJSON
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			log.Printf("config: invalid WALLET_NETWORKS_JSON (%v); using defaults", err)
		} else {
			nets := make([]Network, 0, len(items))
			for _, it := range items {
				nets = append(nets, it.toNetwork())
			}
			return newRegistry(nets)
		}
	}
	return newRegistry(defaultNetworks())
}

// networkJSON is the JSON shape for WALLET_NETWORKS_JSON overrides (string decimals).
type networkJSON struct {
	Asset                   string `json:"asset"`
	Network                 string `json:"network"`
	Confirmations           int    `json:"confirmations"`
	MinDeposit              string `json:"minDeposit"`
	MinWithdraw             string `json:"minWithdraw"`
	MaxWithdraw             string `json:"maxWithdraw"`
	GasFeeEstimate          string `json:"gasFeeEstimate"`
	GasFeeAsset             string `json:"gasFeeAsset"`
	EstimatedArrivalMinutes int    `json:"estimatedArrivalMinutes"`
	AddressPrefix           string `json:"addressPrefix"`
}

func (j networkJSON) toNetwork() Network {
	dec := func(s string) decimal.Decimal {
		if s == "" {
			return decimal.Zero
		}
		return decimal.RequireFromString(s)
	}
	return Network{
		Asset:                   j.Asset,
		Network:                 j.Network,
		Confirmations:           j.Confirmations,
		MinDeposit:              dec(j.MinDeposit),
		MinWithdraw:             dec(j.MinWithdraw),
		MaxWithdraw:             dec(j.MaxWithdraw),
		GasFeeEstimate:          dec(j.GasFeeEstimate),
		GasFeeAsset:             j.GasFeeAsset,
		EstimatedArrivalMinutes: j.EstimatedArrivalMinutes,
		AddressPrefix:           j.AddressPrefix,
	}
}

// defaultNetworks is the built-in registry. Gas estimates follow the business
// rules (TRON ~2, Ethereum ~10, BNB ~1, Arbitrum ~1 USDT); confirmations and
// per-asset min/max for non-USDT assets are not in the rules file and are
// configurable defaults (see the summary). All USDT networks share the 12
// minimum / 20000 maximum withdrawal.
func defaultNetworks() []Network {
	d := decimal.RequireFromString
	usdt := func(network, gas string, conf, mins int, prefix string) Network {
		return Network{
			Asset: "USDT", Network: network, Confirmations: conf,
			MinDeposit: d("1"), MinWithdraw: d("12"), MaxWithdraw: d("20000"),
			GasFeeEstimate: d(gas), GasFeeAsset: "USDT",
			EstimatedArrivalMinutes: mins, AddressPrefix: prefix,
		}
	}
	usdc := func(network, gas string, conf, mins int, prefix string) Network {
		return Network{
			Asset: "USDC", Network: network, Confirmations: conf,
			MinDeposit: d("1"), MinWithdraw: d("1"), MaxWithdraw: d("20000"),
			GasFeeEstimate: d(gas), GasFeeAsset: "USDC",
			EstimatedArrivalMinutes: mins, AddressPrefix: prefix,
		}
	}
	return []Network{
		// USDT — all networks: min 12, max 20000 (business rules).
		usdt("TRC20", "2", 19, 4, "T"),
		usdt("ERC20", "10", 12, 4, "0x"),
		usdt("BEP20", "1", 15, 4, "0x"),
		usdt("ARBITRUM", "1", 20, 3, "0x"),
		// USDC — networks added per the rules file (Solana, Arbitrum One, Ethereum).
		usdc("SOL", "0.5", 32, 1, "So"),
		usdc("ARBITRUM", "1", 20, 3, "0x"),
		usdc("ERC20", "10", 12, 4, "0x"),
		// BTC / ETH / SOL native.
		{Asset: "BTC", Network: "BTC", Confirmations: 2, MinDeposit: d("0.0001"),
			MinWithdraw: d("0.0005"), MaxWithdraw: decimal.Zero, GasFeeEstimate: d("0.0001"),
			GasFeeAsset: "BTC", EstimatedArrivalMinutes: 30, AddressPrefix: "bc1"},
		{Asset: "ETH", Network: "ERC20", Confirmations: 12, MinDeposit: d("0.001"),
			MinWithdraw: d("0.01"), MaxWithdraw: decimal.Zero, GasFeeEstimate: d("0.002"),
			GasFeeAsset: "ETH", EstimatedArrivalMinutes: 10, AddressPrefix: "0x"},
		{Asset: "SOL", Network: "SOL", Confirmations: 32, MinDeposit: d("0.01"),
			MinWithdraw: d("0.02"), MaxWithdraw: decimal.Zero, GasFeeEstimate: d("0.000005"),
			GasFeeAsset: "SOL", EstimatedArrivalMinutes: 1, AddressPrefix: "So"},
	}
}
