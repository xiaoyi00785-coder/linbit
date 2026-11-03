package main

import "testing"

func TestCleanFavSymbols(t *testing.T) {
	got, msg := cleanFavSymbols([]string{"btcusdt", " ETHUSDT", "BTCUSDT"})
	if msg != "" || len(got) != 2 || got[0] != "BTCUSDT" || got[1] != "ETHUSDT" {
		t.Fatalf("got %v %q", got, msg)
	}
	for _, bad := range []string{"BTC/USDT", "", "B", "../x"} {
		if _, msg := cleanFavSymbols([]string{bad}); msg == "" {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, msg := cleanFavSymbols(make([]string, 201)); msg == "" {
		t.Error("201 accepted")
	}
	if m, ok := favMarket(""); !ok || m != "spot" {
		t.Error("default market")
	}
	if _, ok := favMarket("earn"); ok {
		t.Error("earn market accepted")
	}
}
