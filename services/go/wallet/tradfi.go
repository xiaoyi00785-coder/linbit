package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// TradFi tokens (xStocks / Ondo / PAXG / XAUT) are configured in admin and
// stored by market-data in tradfi_tokens (same Postgres database). At startup
// every row with a contract address and deposit or withdraw enabled is merged
// into the network registry; enabling a new token therefore needs a wallet
// restart. Disabling takes effect immediately via tradfiAllowed (live read).

// tradfiTokenSpecs maps pairKey(asset, network) -> "contract:decimals"; used by
// RealChain when no WALLET_TOKEN_<ASSET>_<NET> env override is set.
var tradfiTokenSpecs = map[string]string{}

var tradfiNetDefaults = map[string]struct {
	conf, minutes int
	prefix        string
}{
	"ERC20": {12, 4, "0x"}, "TRC20": {19, 4, "T"}, "SOL": {32, 1, "So"},
	"BEP20": {15, 4, "0x"}, "ARBITRUM": {20, 3, "0x"},
}

func mergeTradFiNetworks(ctx context.Context, db *sql.DB, reg NetworkRegistry) NetworkRegistry {
	// Mainnet: only contracts ops verified (contract_verified, migration
	// market-data 00009) are loaded; testnet leftovers stay inert.
	rows, err := db.QueryContext(ctx, `SELECT asset, network, contract_address, decimals, min_withdraw::text, withdraw_fee::text
		FROM tradfi_tokens WHERE contract_address <> '' AND (deposit_enabled OR withdraw_enabled)`+tradfiVerifiedClause(common.IsMainnet())+`
		ORDER BY asset, issuer, network`)
	if err != nil {
		log.Printf("wallet: tradfi tokens not loaded (%v)", err) // table absent before market-data migrates
		return reg
	}
	defer rows.Close()
	nets := []Network{}
	for _, ns := range reg.byAsset {
		nets = append(nets, ns...)
	}
	added := 0
	for rows.Next() {
		var asset, network, addr, minW, fee string
		var dec int
		if err := rows.Scan(&asset, &network, &addr, &dec, &minW, &fee); err != nil {
			log.Printf("wallet: tradfi scan: %v", err)
			return reg
		}
		asset, network = strings.ToUpper(asset), strings.ToUpper(network)
		d, ok := tradfiNetDefaults[network]
		if !ok {
			log.Printf("wallet: tradfi %s/%s: unsupported network, skipped", asset, network)
			continue
		}
		if _, dup := reg.Lookup(asset, network); dup || tradfiTokenSpecs[pairKey(asset, network)] != "" {
			log.Printf("wallet: tradfi %s/%s already configured, skipped duplicate", asset, network)
			continue
		}
		mw, fe := decimal.RequireFromString(minW), decimal.RequireFromString(fee)
		nets = append(nets, Network{Asset: asset, Network: network, Confirmations: d.conf,
			MinDeposit: decimal.Zero, MinWithdraw: mw, MaxWithdraw: decimal.Zero,
			GasFeeEstimate: fe, GasFeeAsset: asset, EstimatedArrivalMinutes: d.minutes, AddressPrefix: d.prefix})
		tradfiTokenSpecs[pairKey(asset, network)] = addr + ":" + strconv.Itoa(dec)
		added++
	}
	if added > 0 {
		log.Printf("wallet: merged %d tradfi token network(s)", added)
	}
	return newRegistry(nets)
}

// tradfiAllowed reports whether a deposit (withdraw=false) or withdrawal of a
// TradFi token is currently enabled. Non-TradFi pairs always pass.
func (s *Server) tradfiAllowed(ctx context.Context, asset, network string, withdraw bool) bool {
	if tradfiTokenSpecs[pairKey(asset, network)] == "" {
		return true
	}
	var dep, wd bool
	err := s.store.db.QueryRowContext(ctx, `SELECT bool_or(deposit_enabled AND contract_address<>''), bool_or(withdraw_enabled AND contract_address<>'')
		FROM tradfi_tokens WHERE asset=$1 AND network=$2`, strings.ToUpper(asset), strings.ToUpper(network)).Scan(&dep, &wd)
	if err != nil {
		return false // fail closed
	}
	if withdraw {
		return wd
	}
	return dep
}

var errTradFiDisabled = errf(http.StatusForbidden, "NETWORK_DISABLED", "该网络当前暂停")

func tradfiVerifiedClause(mainnet bool) string {
	if mainnet {
		return " AND contract_verified"
	}
	return ""
}
