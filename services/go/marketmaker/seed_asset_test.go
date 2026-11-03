package main

import "testing"

func TestSeedAssetAllowed(t *testing.T) {
	base := "BTC, ETH,SOL"
	for _, c := range []struct {
		a  string
		ok bool
	}{{"USDT", true}, {"ETH", true}, {"SOL", true}, {"DOGE", false}, {"", false}} {
		if got := SeedAssetAllowed(c.a, base); got != c.ok {
			t.Errorf("%q: got %v", c.a, got)
		}
	}
}
