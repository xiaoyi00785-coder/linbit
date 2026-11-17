package main

import (
	"testing"
)

func TestPickTakesHitsBestBidAsk(t *testing.T) {
	r := Rules{Step: d("0.001"), MinQty: d("0.001")}
	targets := []Quote{
		{Side: "buy", Price: d("99"), Qty: d("1"), Level: 2},
		{Side: "buy", Price: d("100"), Qty: d("2"), Level: 1},
		{Side: "sell", Price: d("102"), Qty: d("3"), Level: 2},
		{Side: "sell", Price: d("101"), Qty: d("1.5"), Level: 1},
	}
	got := pickTakes(targets, d("50"), r)
	if len(got) != 2 {
		t.Fatalf("want 2 takes, got %+v", got)
	}
	var buy, sell Quote
	for _, q := range got {
		if q.Side == "buy" {
			buy = q
		} else {
			sell = q
		}
	}
	if !buy.Price.Equal(d("101")) { // take the best ask
		t.Fatalf("buy price %s", buy.Price)
	}
	if !sell.Price.Equal(d("100")) { // take the best bid
		t.Fatalf("sell price %s", sell.Price)
	}
	if !buy.Qty.Equal(sell.Qty) || !buy.Qty.IsPositive() {
		t.Fatalf("qty buy=%s sell=%s", buy.Qty, sell.Qty)
	}
}

func TestPickTakesEmpty(t *testing.T) {
	if got := pickTakes(nil, d("10"), Rules{Step: d("0.001")}); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestSplitAssetsDropsUSDT(t *testing.T) {
	got := splitAssets(" BTC, usdt, ETH, BTC, ")
	if len(got) != 2 || got[0] != "BTC" || got[1] != "ETH" {
		t.Fatalf("%v", got)
	}
}
