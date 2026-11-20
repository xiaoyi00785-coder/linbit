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
	for _, p := range []string{"/convert/pairs", "/event/products",
		"/strategy/grid/square", "/earn/products", "/earn/products/simple-USDT",
		"/staking/validators", "/staking/validators/stk-ETH-lido",
		"/academy/categories", "/academy/articles/1", "/cards/product", "/fiat/buy", "/support/guest", "/auth/providers", "/market/search", "/market/usd-rate",
		"/campaigns/deposit-cashback", "/campaigns/trade-lottery"} {
		if !g.isPublic(httptest.NewRequest("GET", p, nil)) {
			t.Errorf("GET %s should be public", p)
		}
	}
	if !g.isPublic(httptest.NewRequest("POST", "/auth/google", nil)) {
		t.Error("POST /auth/google should be public")
	}
	for _, c := range [][2]string{{"POST", "/convert/quotes"}, {"GET", "/event/orders"}, {"GET", "/chat/rooms"}, {"GET", "/chat/rooms/general/messages"}, {"POST", "/chat/rooms/BTCUSDT/messages"},
		{"GET", "/strategy/grid/bots"}, {"GET", "/earn/positions"},
		{"GET", "/staking/positions"}, {"POST", "/staking/stake"}, {"POST", "/staking/unstake"}, {"POST", "/staking/claim"},
		{"POST", "/cards/applications"}, {"POST", "/fiat/buy/quotes"}, {"GET", "/cards/application"}, {"GET", "/futures/pnl/history"},
		{"POST", "/campaigns/deposit-cashback/tiers/1/join"}, {"GET", "/campaigns/trade-lottery/entries"}, {"POST", "/account/api-keys"}} {
		if g.isPublic(httptest.NewRequest(c[0], c[1], nil)) {
			t.Errorf("%s %s must stay authed", c[0], c[1])
		}
	}
}
