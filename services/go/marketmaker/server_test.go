package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// MM credit is no longer blocked by NETWORK/WALLET_CHAIN. Without a live
// asset service the call fails upstream, but it must not return SEED_DISABLED.
func TestSeedAllowedOnMainnet(t *testing.T) {
	m := &Maker{cfg: Config{UserID: "mm:main", SeedBaseAssets: "BTC,ETH"}}
	t.Setenv("LINBIT_ENV", "production")
	t.Setenv("WALLET_CHAIN", "mainnet")
	t.Setenv("NETWORK", "mainnet")
	req := httptest.NewRequest(http.MethodPost, "/internal/mm/seed", strings.NewReader(`{"ref":"x","asset":"USDT","amount":"1000"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.Routes().ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "SEED_DISABLED") || w.Code == http.StatusForbidden {
		t.Fatalf("mainnet still blocked seed: %d %s", w.Code, w.Body.String())
	}
}
