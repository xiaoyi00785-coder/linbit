package main

import (
	"net/http/httptest"
	"testing"
)

// Futures info pages read these without a token.
func TestFuturesInfoPublic(t *testing.T) {
	g, err := NewGateway(Config{JWTSecret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/futures/market/risk-tiers", "/futures/market/isolated-params", "/futures/market/index?symbol=BTCUSDT",
		"/futures/market/mark-history?symbol=BTCUSDT&interval=1h", "/futures/market/funding-history?symbol=BTCUSDT",
		"/futures/market/insurance-fund", "/futures/market/insurance-fund/history"} {
		if !g.isPublic(httptest.NewRequest("GET", p, nil)) {
			t.Errorf("GET %s should be public", p)
		}
	}
	if g.isPublic(httptest.NewRequest("POST", "/futures/market/risk-tiers", nil)) {
		t.Error("POST must not be public")
	}
}
