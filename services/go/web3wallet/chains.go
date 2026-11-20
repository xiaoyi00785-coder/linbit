package main

import (
	"fmt"
	"strings"
)

// Supported chain identifiers (the string the client sends on the wire).
const (
	ChainETH      = "ethereum"
	ChainBSC      = "bsc"
	ChainPolygon  = "polygon"
	ChainArbitrum = "arbitrum"
	ChainBase     = "base"
	ChainSolana   = "solana"
	ChainTRON     = "tron"
	ChainBTC      = "bitcoin"
)

type ChainKind int

const (
	KindEVM ChainKind = iota + 1
	KindBTC
	KindSolana
	KindTRON
)

// ChainInfo pins the derivation path, native symbol and kind for each chain.
// BIP44 coin types from SLIP-44: ETH=60, BSC/Polygon/Arbitrum/Base piggyback
// on 60, BTC=0, SOL=501, TRON=195.
type ChainInfo struct {
	Name          string
	Kind          ChainKind
	Symbol        string // native asset symbol
	Decimals      int
	BIP44         string // m/44'/coin'/0'/0/0
	EVMChainID    int64  // 0 for non-EVM
	ExplorerTxURL string
}

var chainCatalog = map[string]ChainInfo{
	ChainETH:      {ChainETH, KindEVM, "ETH", 18, "m/44'/60'/0'/0/0", 1, "https://etherscan.io/tx/"},
	ChainBSC:      {ChainBSC, KindEVM, "BNB", 18, "m/44'/60'/0'/0/0", 56, "https://bscscan.com/tx/"},
	ChainPolygon:  {ChainPolygon, KindEVM, "MATIC", 18, "m/44'/60'/0'/0/0", 137, "https://polygonscan.com/tx/"},
	ChainArbitrum: {ChainArbitrum, KindEVM, "ETH", 18, "m/44'/60'/0'/0/0", 42161, "https://arbiscan.io/tx/"},
	ChainBase:     {ChainBase, KindEVM, "ETH", 18, "m/44'/60'/0'/0/0", 8453, "https://basescan.org/tx/"},
	ChainBTC:      {ChainBTC, KindBTC, "BTC", 8, "m/84'/0'/0'/0/0", 0, "https://blockstream.info/tx/"},
	ChainSolana:   {ChainSolana, KindSolana, "SOL", 9, "m/44'/501'/0'/0'", 0, "https://solscan.io/tx/"},
	ChainTRON:     {ChainTRON, KindTRON, "TRX", 6, "m/44'/195'/0'/0/0", 0, "https://tronscan.org/#/transaction/"},
}

func AllChains() []string {
	// Deterministic order to keep responses stable.
	return []string{ChainETH, ChainBSC, ChainPolygon, ChainArbitrum, ChainBase, ChainBTC, ChainSolana, ChainTRON}
}

func LookupChain(name string) (ChainInfo, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	info, ok := chainCatalog[name]
	if !ok {
		return ChainInfo{}, fmt.Errorf("unsupported chain %q", name)
	}
	return info, nil
}

func IsEVM(name string) bool {
	info, err := LookupChain(name)
	return err == nil && info.Kind == KindEVM
}
