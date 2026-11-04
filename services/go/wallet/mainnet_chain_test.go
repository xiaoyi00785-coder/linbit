package main

import (
	"strings"
	"testing"
)

func TestMainnetChainProblems(t *testing.T) {
	t.Setenv("NETWORK", "mainnet")
	t.Setenv("WALLET_NETWORKS_JSON", `[{"asset":"USDT","network":"TRC20"},{"asset":"BTC","network":"BTC"}]`)
	t.Setenv("WALLET_RPC_TRC20", "https://rpc.example")
	t.Setenv("WALLET_RPC_BTC", "")
	t.Setenv("WALLET_COLD_ADDRESS_BTC", "")
	t.Setenv("WALLET_COLD_ADDRESS_TRC20", "T-cold")
	p := strings.Join(mainnetChainProblems(), ";")
	if !strings.Contains(p, "WALLET_RPC_BTC") || !strings.Contains(p, "WALLET_COLD_ADDRESS_BTC") {
		t.Fatalf("want BTC rpc+cold problems, got %q", p)
	}
	if strings.Contains(p, "TRC20") || strings.Contains(p, "ARBITRUM") {
		t.Fatalf("unexpected: %q", p)
	}
	t.Setenv("NETWORK", "testnet")
	t.Setenv("APP_ENV", "")
	if len(mainnetChainProblems()) != 0 {
		t.Fatal("off mainnet must be no-op")
	}
}
