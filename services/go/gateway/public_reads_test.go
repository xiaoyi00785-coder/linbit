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
		"/strategy/grid/square", "/earn/products", "/earn/products/simple-USDT"} {
		if !g.isPublic(httptest.NewRequest("GET", p, nil)) {
			t.Errorf("GET %s should be public", p)
		}
	}
	for _, c := range [][2]string{{"POST", "/convert/quotes"}, {"GET", "/event/orders"}, {"POST", "/chat/rooms/BTCUSDT/messages"},
		{"GET", "/strategy/grid/bots"}, {"GET", "/earn/positions"}} {
		if g.isPublic(httptest.NewRequest(c[0], c[1], nil)) {
			t.Errorf("%s %s must stay authed", c[0], c[1])
		}
	}
}
