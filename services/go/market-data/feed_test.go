package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestIndexFeedFailover(t *testing.T) {
	var binanceDown atomic.Bool
	bn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if binanceDown.Load() {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v3/exchangeInfo") {
			w.Write([]byte(`{"symbols":[{"symbol":"BTCUSDT","status":"TRADING"},{"symbol":"ETHUSDT","status":"TRADING"}]}`))
			return
		}
		w.Write([]byte(`[{"symbol":"BTCUSDT","price":"60000.10"},{"symbol":"ETHUSDT","price":"3000"}]`))
	}))
	defer bn.Close()
	var okxDown atomic.Bool
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if okxDown.Load() {
			w.WriteHeader(500)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v5/public/instruments") {
			w.Write([]byte(`{"code":"0","data":[{"instId":"BTC-USDT","state":"live"}]}`))
			return
		}
		w.Write([]byte(`{"code":"0","data":[{"instId":"BTC-USDT","last":"60001"}]}`))
	}))
	defer ok.Close()
	cfg := Config{IndexFeed: []string{"binance", "okx"}, IndexPoll: 20 * time.Millisecond, IndexStale: 150 * time.Millisecond, BinanceURL: bn.URL, OKXURL: ok.URL}
	f := NewIndexFeed(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	wait := func(want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			r, okk := f.Index("BTCUSDT")
			if (want == "" && !okk) || (okk && r.Source == want) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		r, okk := f.Index("BTCUSDT")
		t.Fatalf("want source %q, got %+v ok=%v", want, r, okk)
	}
	wait("binance")
	if r, _ := f.Index("btcusdt"); r.Price != "60000.1" {
		t.Fatalf("price %s", r.Price)
	}
	binanceDown.Store(true)
	wait("okx")
	okxDown.Store(true)
	wait("") // fail closed
	binanceDown.Store(false)
	wait("binance")
	f.SetOutage(true)
	wait("")
	f.SetOutage(false)
	f.SetOverride("BTCUSDT", decimal.NewFromInt(5))
	wait("dev-override")
}

func TestIndexFeedLocal(t *testing.T) {
	f := NewIndexFeed(Config{}, func(s string) (decimal.Decimal, bool) { return decimal.NewFromInt(7), s == "BTCUSDT" })
	if r, ok := f.Index("BTCUSDT"); !ok || r.Source != "local" || r.Price != "7" {
		t.Fatalf("%+v %v", r, ok)
	}
	if _, ok := f.Index("XUSDT"); ok {
		t.Fatal("expected unavailable")
	}
}

func TestParseFeed(t *testing.T) {
	if got := parseFeed("binance, OKX"); len(got) != 2 || got[1] != "okx" {
		t.Fatal(got)
	}
	if got := parseFeed("binance,okx,Bybit,gate,gateio"); len(got) != 5 || got[2] != "bybit" || got[3] != "gateio" {
		t.Fatal(got)
	}
	if parseFeed("off") != nil {
		t.Fatal("off")
	}
}
