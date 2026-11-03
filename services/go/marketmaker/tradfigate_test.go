package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestGateBlock(t *testing.T) {
	g := &tradfiGate{Features: map[string]bool{"tokenizedStocks": false, "metals": true},
		SpotSymbols:    map[string]string{"AAPLUSDT": "tokenizedStocks", "PAXGUSDT": "metals", "NEWSTKUSDT": "tokenizedStocks"},
		FuturesSymbols: map[string]string{"AAPLUSDT-PERP": "tokenizedStocks"}}
	cases := []struct {
		g         *tradfiGate
		ok        bool
		mkt, sym  string
		wantBlock bool
	}{
		{g, true, "spot", "AAPLUSDT", true},
		{g, true, "spot", "newstkusdt", true},
		{g, true, "spot", "PAXGUSDT", false},
		{g, true, "spot", "BTCUSDT", false},
		{g, true, "futures", "AAPLUSDT-PERP", true},
		{g, true, "futures", "BTCUSDT", false},
		{g, false, "spot", "PAXGUSDT", true},   // stale gate -> fail closed
		{nil, false, "spot", "TSLAUSDT", true}, // never loaded: known seed list
		{nil, false, "spot", "ETHUSDT", false}, // crypto unaffected
	}
	for _, c := range cases {
		if got := gateBlock(c.g, c.ok, c.mkt, c.sym) != ""; got != c.wantBlock {
			t.Errorf("%s/%s ok=%v: blocked=%v want %v", c.mkt, c.sym, c.ok, got, c.wantBlock)
		}
	}
}

func TestOneSidedReason(t *testing.T) {
	d := decimal.RequireFromString
	p := Pair{TargetBaseRatio: d("0.5")}
	if r := oneSidedReason(spotInventory(p, d("200"), d("0"), d("1000"), Baseline{})); r == "" {
		t.Error("unseeded zero-base pair should pause")
	}
	inv := spotInventory(p, d("200"), d("0"), d("1000"), Baseline{Qty: d("5")})
	if r := oneSidedReason(inv); r != "" || !inv.AllowBuy || inv.AllowSell {
		t.Errorf("seeded but sold out should keep bidding: %q %+v", r, inv)
	}
	if r := oneSidedReason(spotInventory(p, d("200"), d("1"), d("1000"), Baseline{})); r != "" {
		t.Errorf("has base: %q", r)
	}
}
