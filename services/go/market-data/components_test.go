package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestIndexComponents(t *testing.T) {
	f := &IndexFeed{stale: time.Minute, tradfi: map[string]tradfiRef{}, override: map[string]decimal.Decimal{}}
	now := time.Now()
	f.sources = []*indexSource{
		{name: "binance", quotes: map[string]indexQuote{"BTCUSDT": {price: decimal.RequireFromString("60000"), at: now}}},
		{name: "okx", quotes: map[string]indexQuote{"BTCUSDT": {price: decimal.RequireFromString("60001"), at: now.Add(-2 * time.Minute)}}},
	}
	cs := f.Components("btcusdt", "binance")
	if len(cs) != 2 || cs[0].Weight != "1" || !cs[0].Fresh || cs[0].Price != "60000" {
		t.Fatalf("binance: %+v", cs)
	}
	if cs[1].Weight != "0" || cs[1].Fresh || cs[1].Price != "60001" {
		t.Fatalf("okx: %+v", cs)
	}
}
