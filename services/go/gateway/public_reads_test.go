package main

import (
	"net/http/httptest"
	"testing"
)

// Mobile anonymous reads: these GETs must pass without a token; the
// user-specific neighbours must stay authed.
func TestMobilePublicReads(t *testing.T) {
	g, err := NewGateway(Config{JWTSecret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/convert/pairs", "/event/products", "/chat/rooms", "/chat/rooms/BTCUSDT/messages",
		"/strategy/grid/square", "/earn/products", "/earn/products/simple-USDT",
		"/academy/categories", "/academy/articles/1", "/cards/product", "/fiat/buy", "/support/guest", "/auth/providers", "/market/search", "/market/usd-rate"} {
		if !g.isPublic(httptest.NewRequest("GET", p, nil)) {
			t.Errorf("GET %s should be public", p)
		}
	}
	if !g.isPublic(httptest.NewRequest("POST", "/auth/google", nil)) {
		t.Error("POST /auth/google should be public")
	}
	for _, c := range [][2]string{{"POST", "/convert/quotes"}, {"GET", "/event/orders"}, {"POST", "/chat/rooms/BTCUSDT/messages"},
		{"GET", "/strategy/grid/bots"}, {"GET", "/earn/positions"},
		{"POST", "/cards/applications"}, {"POST", "/fiat/buy/quotes"}, {"GET", "/cards/application"}, {"GET", "/futures/pnl/history"}} {
		if g.isPublic(httptest.NewRequest(c[0], c[1], nil)) {
			t.Errorf("%s %s must stay authed", c[0], c[1])
		}
	}
}
