package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

// acceptance r8: a dust BTC balance showed price 85,714.29 because the web divided
// the 2-dp valuation by the quantity. The overview must carry the live index price.
func TestOverviewCarriesIndexPrice(t *testing.T) {
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"indexPrice":"78512.34"}`))
	}))
	defer md.Close()
	s := &Server{cfg: Config{MarketURL: md.URL}}
	acc := s.valued(context.Background(), "funding", []Balance{{Asset: "BTC", Free: decimal.RequireFromString("0.0000007"), Locked: decimal.Zero}}, map[string]*decimal.Decimal{})
	b := acc.Balances[0]
	if b.PriceUsd == nil || *b.PriceUsd != "78512.34" {
		t.Fatalf("priceUsd=%v", b.PriceUsd)
	}
	if b.ValuationUsd == nil || *b.ValuationUsd != "0.05" {
		t.Fatalf("valuation=%v", *b.ValuationUsd)
	}
}
