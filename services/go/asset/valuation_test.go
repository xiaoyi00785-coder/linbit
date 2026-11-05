package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func valBal(asset, free, locked string) Balance {
	return Balance{Asset: asset, Free: decimal.RequireFromString(free), Locked: decimal.RequireFromString(locked)}
}

// strictTotal values every held asset and reports complete=false (lower bound)
// when a held asset has no index price; zero balances never block completeness.
func TestStrictTotal(t *testing.T) {
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.RawQuery, "BTCUSDT"):
			w.Write([]byte(`{"indexPrice":"60000"}`))
		default: // TONUSDT etc: feed has no usable index
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":"INDEX_UNAVAILABLE"}`))
		}
	}))
	defer md.Close()
	s := &Server{cfg: Config{MarketURL: md.URL}}
	ctx := context.Background()

	total, complete := s.strictTotal(ctx, map[string]*decimal.Decimal{},
		[]Balance{valBal("USDT", "1000", "500"), valBal("BTC", "1", "0.5")},
		[]Balance{valBal("TON", "0", "0")}, // zero balance: no price needed
		[]Balance{valBal("USDT", "250.5", "0")})
	if !complete || !total.Equal(decimal.RequireFromString("91750.5")) {
		t.Fatalf("total=%s complete=%v", total, complete)
	}

	total, complete = s.strictTotal(ctx, map[string]*decimal.Decimal{},
		[]Balance{valBal("USDT", "100", "0"), valBal("TON", "5000", "0")})
	if complete || !total.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("unpriced held asset must make the total a lower bound: total=%s complete=%v", total, complete)
	}

	// no market URL configured: only USDT can be valued
	s = &Server{}
	total, complete = s.strictTotal(ctx, map[string]*decimal.Decimal{}, []Balance{valBal("USDT", "7", "0"), valBal("BTC", "1", "0")})
	if complete || !total.Equal(decimal.NewFromInt(7)) {
		t.Fatalf("total=%s complete=%v", total, complete)
	}
}
