package main

import "testing"

func TestFilterTickersByChange(t *testing.T) {
	in := []tickerDTO{{Symbol: "A", PriceChangePercent: "-1"}, {Symbol: "B", PriceChangePercent: "-0.5"}, {Symbol: "C", PriceChangePercent: "0"}, {Symbol: "SPY", PriceChangePercent: "0.10"}}
	l := filterTickersByChange(append([]tickerDTO(nil), in...), -1)
	if len(l) != 2 || l[0].Symbol != "A" || l[1].Symbol != "B" {
		t.Fatalf("losers=%+v", l)
	}
	g := filterTickersByChange(append([]tickerDTO(nil), in...), 1)
	if len(g) != 1 || g[0].Symbol != "SPY" {
		t.Fatalf("gainers=%+v", g)
	}
}
