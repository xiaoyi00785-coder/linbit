package main

import "testing"

func TestValidateSingleSeed(t *testing.T) {
	cases := []struct {
		asset, amount string
		assets        []string
		reason        string
		ok            bool
	}{
		{"USDT", "5000000", nil, "testnet top-up", true},
		{"USDT", "10000000", nil, "r", true},
		{"USDT", "10000000.00000001", nil, "r", false}, // over max
		{"USDT", "5000000", nil, "", false},            // reason required
		{"USDT", "0", nil, "r", false},
		{"USDT", "-1", nil, "r", false},
		{"USDT", "abc", nil, "r", false},
		{"USDT", "1.123456789", nil, "r", false}, // >8dp
		{"", "1", nil, "r", false},
		{"US-DT", "1", nil, "r", false},
		{"USDT", "1", []string{"BTC"}, "r", false}, // mixed modes
	}
	for _, c := range cases {
		if got := validateSingleSeed(c.asset, c.amount, c.assets, c.reason, "10000000") == ""; got != c.ok {
			t.Errorf("%+v: ok=%v", c, got)
		}
	}
	if validateSingleSeed("USDT", "6000000", nil, "r", "5000000") == "" {
		t.Error("configured max not enforced")
	}
}
