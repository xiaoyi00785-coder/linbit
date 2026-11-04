package common

import (
	"strings"
	"testing"
)

func TestMainnetProblems(t *testing.T) {
	env := map[string]string{"WALLET_CHAIN": "testnet", "NEXT_PUBLIC_ENABLE_MOCK": "true", "MM_TESTNET_SEED": "1"}
	p := strings.Join(MainnetProblems(func(k string) string { return env[k] }), ";")
	for _, want := range []string{"WALLET_CHAIN", "NEXT_PUBLIC_ENABLE_MOCK", "MM_TESTNET_SEED"} {
		if !strings.Contains(p, want) {
			t.Fatalf("missing %s in %s", want, p)
		}
	}
	ok := map[string]string{"WALLET_CHAIN": "mainnet", "NEXT_PUBLIC_ENABLE_MOCK": "false"}
	if p := MainnetProblems(func(k string) string { return ok[k] }); len(p) != 0 {
		t.Fatalf("unexpected %v", p)
	}
}

func TestSeedForbiddenOnMainnet(t *testing.T) {
	t.Setenv("NETWORK", "mainnet")
	t.Setenv("WALLET_CHAIN", "testnet")
	if ok, _ := TestnetSeedAllowed(); ok {
		t.Fatal("seed must be refused on mainnet")
	}
	if !IsProduction() {
		t.Fatal("mainnet implies production")
	}
}
