package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"
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
	// Admin-controllable flags (zero value = enabled / no memo) so existing
	// literal Network values keep their behaviour.
	MemoRequired     bool
	DepositDisabled  bool
	WithdrawDisabled bool
	// Display hints for the network picker (admin-overridable).
	Recommended bool
	SortOrder   int // ascending within an asset; ties broken by network code
}

// NetworkRegistry indexes networks by asset and by (asset, network).
type NetworkRegistry struct {
	byAsset map[string][]Network
	byPair  map[string]Network
}

// Lookup returns the Network for an (asset, network) pair.
func (r NetworkRegistry) Lookup(asset, network string) (Network, bool) {
	n, ok := r.byPair[pairKey(asset, network)]
	if ok {
		n = applyOverlay(n)
	}
	return n, ok
}

// ForAsset returns all networks configured for an asset (nil if none).
func (r NetworkRegistry) ForAsset(asset string) []Network {
	src := r.byAsset[strings.ToUpper(asset)]
	if src == nil {
		return nil
	}
	out := make([]Network, len(src))
	for i, n := range src {
		out[i] = applyOverlay(n)
	}
	return out
}

// All returns every configured network with admin overrides applied.
func (r NetworkRegistry) All() []Network {
	out := make([]Network, 0, len(r.byPair))
	for _, list := range r.byAsset {
		for _, n := range list {
			out = append(out, applyOverlay(n))
		}
	}
	return out
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
			if common.IsMainnet() {
				log.Fatalf("config: invalid WALLET_NETWORKS_JSON on mainnet (%v); refusing to start", err)
			}
			log.Printf("config: invalid WALLET_NETWORKS_JSON (%v); using defaults", err)
		} else {
			nets := make([]Network, 0, len(items))
			rec, srt := make([]bool, len(items)), make([]bool, len(items))
			for i, it := range items {
				n := it.toNetwork()
				if it.Recommended != nil {
					n.Recommended, rec[i] = *it.Recommended, true
				}
				if it.SortOrder != nil {
					n.SortOrder, srt[i] = *it.SortOrder, true
				}
				nets = append(nets, n)
			}
			applyDisplayDefaults(nets, rec, srt)
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
	MemoRequired            bool   `json:"memoRequired"`
	DepositDisabled         bool   `json:"depositDisabled"`
	WithdrawDisabled        bool   `json:"withdrawDisabled"`
	Recommended             *bool  `json:"recommended"` // nil = default (USDT/TRC20)
	SortOrder               *int   `json:"sortOrder"`   // nil = declaration order within the asset
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
		MemoRequired:            j.MemoRequired,
		DepositDisabled:         j.DepositDisabled,
		WithdrawDisabled:        j.WithdrawDisabled,
	}
}

// defaultRecommended: the built-in picker recommendation when nothing is configured.
func defaultRecommended(asset, network string) bool {
	return strings.EqualFold(asset, "USDT") && strings.EqualFold(network, "TRC20")
}

// applyDisplayDefaults fills Recommended / SortOrder for entries without an
// explicit value: sortOrder = 10, 20, ... in declaration order per asset.
func applyDisplayDefaults(nets []Network, explicitRec, explicitSort []bool) {
	seen := map[string]int{}
	for i := range nets {
		a := strings.ToUpper(nets[i].Asset)
		seen[a]++
		if !explicitSort[i] {
			nets[i].SortOrder = seen[a] * 10
		}
		if !explicitRec[i] {
			nets[i].Recommended = defaultRecommended(nets[i].Asset, nets[i].Network)
		}
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
	nets := []Network{
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
	none := make([]bool, len(nets))
	applyDisplayDefaults(nets, none, none)
	return nets
}

// mainnetChainProblems: on mainnet every chain enabled in WALLET_NETWORKS_JSON
// needs its own RPC and an explicit cold address (names only, never values).
// Chains not listed (e.g. ARBITRUM) need nothing.
func mainnetChainProblems() []string {
	if !common.IsMainnet() {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range loadNetworks().byPair {
		name := n.Network
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, known := realNetDefaults[name]; !known {
			out = append(out, "WALLET_NETWORKS_JSON: unknown network "+name)
			continue
		}
		for _, k := range []string{envKey("WALLET_RPC", name), envKey("WALLET_COLD_ADDRESS", name)} {
			if strings.TrimSpace(os.Getenv(k)) == "" {
				out = append(out, k+" is required on mainnet ("+name+" is enabled)")
			}
		}
	}
	sort.Strings(out)
	return out
}
