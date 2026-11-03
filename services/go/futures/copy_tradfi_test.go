package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCopyTradFiBlocked(t *testing.T) {
	flags := map[string]bool{"metals": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/tradfi/gate":
			json.NewEncoder(w).Encode(map[string]any{"features": flags, "restrictedCountries": []string{"US"},
				"futuresSymbols": map[string]string{"XAGUSDT": "metals", "WTIUSDT": "commodities"}})
		case "/internal/kyc/country":
			c := ""
			if r.URL.Query().Get("userId") == "u-us" {
				c = "us"
			}
			json.NewEncoder(w).Encode(map[string]string{"country": c})
		}
	}))
	defer srv.Close()
	g := newCopyTradFi(srv.URL, srv.URL, "")
	ctx := context.Background()
	for _, c := range []struct{ sym, uid, want string }{
		{"BTCUSDT", "u-us", ""},
		{"XAGUSDT", "u1", ""},
		{"XAGUSDT", "u-us", "REGION_RESTRICTED"},
		{"WTIUSDT", "u1", "FEATURE_DISABLED"},
	} {
		if got := g.blocked(ctx, c.sym, c.uid); got != c.want {
			t.Errorf("%s/%s: got %q want %q", c.sym, c.uid, got, c.want)
		}
	}
	down := newCopyTradFi("http://127.0.0.1:1", "http://127.0.0.1:1", "")
	if got := down.blocked(ctx, "XAGUSDT", "u1"); got != "TRADFI_UNAVAILABLE" {
		t.Errorf("fail closed: got %q", got)
	}
	if got := down.blocked(ctx, "BTCUSDT", "u1"); got != "" {
		t.Errorf("crypto when gate down: got %q", got)
	}
}
