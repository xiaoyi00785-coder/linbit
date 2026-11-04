package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

type fakeBook struct {
	ask string
	err error
}

func (f fakeBook) Submit(context.Context, EngineCommand) (*ExecReport, error) { return nil, nil }
func (f fakeBook) BestAsk(context.Context, string) (string, error)          { return f.ask, f.err }

type noBook struct{}

func (noBook) Submit(context.Context, EngineCommand) (*ExecReport, error) { return nil, nil }

func btcSym() *SymbolConfig {
	return &SymbolConfig{Symbol: "BTCUSDT", Base: "BTC", Quote: "USDT", PricePrecision: 2, QuantityPrecision: 5,
		MinQuantity: decimal.RequireFromString("0.00001"), MinNotional: decimal.RequireFromString("5")}
}

func TestMarketBuyByQtyErrors(t *testing.T) {
	cases := []struct {
		name string
		eng  Matching
		qty  string
		code string
	}{
		{"no liquidity", fakeBook{ask: ""}, "0.01", "NO_LIQUIDITY"},
		{"engine down", fakeBook{err: errors.New("boom")}, "0.01", "ENGINE_UNAVAILABLE"},
		{"no booktop", noBook{}, "0.01", "VALIDATION_ERROR"},
		{"bad qty", fakeBook{ask: "100"}, "-1", "VALIDATION_ERROR"},
		{"precision", fakeBook{ask: "100"}, "0.0000001", "VALIDATION_ERROR"},
		{"min notional", fakeBook{ask: "100"}, "0.01", "VALIDATION_ERROR"}, // 1 USDT < 5
	}
	for _, c := range cases {
		s := &Server{engine: c.eng}
		_, _, code, _ := s.marketBuyByQty(context.Background(), &Order{}, btcSym(), c.qty)
		if code != c.code {
			t.Errorf("%s: code=%q want %q", c.name, code, c.code)
		}
	}
}

func TestHTTPMatchingBestAsk(t *testing.T) {
	body := `{"bids":[{"price":"99","quantity":"1"}],"asks":[{"price":"101.5","quantity":"2"},{"price":"102","quantity":"1"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/engine/depth" || r.URL.Query().Get("symbol") != "BTCUSDT" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	m := NewHTTPMatching(srv.URL, "")
	if a, err := m.BestAsk(context.Background(), "BTCUSDT"); err != nil || a != "101.5" {
		t.Fatalf("ask=%q err=%v", a, err)
	}
	body = `{"bids":[],"asks":[]}`
	if a, err := m.BestAsk(context.Background(), "BTCUSDT"); err != nil || a != "" {
		t.Fatalf("empty ask=%q err=%v", a, err)
	}
	if _, err := m.BestAsk(context.Background(), "NOPE"); err == nil {
		t.Fatal("want error on 404")
	}
}
