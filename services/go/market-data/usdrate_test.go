package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestInstrumentUSDNotPegged(t *testing.T) {
	px, reason := instrumentUSD("BTCUSDT", "100", decimal.RequireFromString("0.9985"))
	if reason != "" || px != "99.85000000" {
		t.Fatalf("price=%s reason=%s", px, reason)
	}
	if _, reason := instrumentUSD("BTCUSDT", "100", decimal.Zero); reason != "rate_unavailable" {
		t.Fatalf("missing rate: %s", reason)
	}
	if _, reason := instrumentUSD("BTCUSD", "100", decimal.NewFromInt(1)); reason != "quote_not_usdt" {
		t.Fatalf("non usdt quote: %s", reason)
	}
}

func TestFetchUSDTUSD(t *testing.T) {
	resetUSDCache()
	var coin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/USDT-USD/ticker" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.Write([]byte(`{"price":"` + coin + `","time":"2026-11-08T00:00:00Z"}`))
	}))
	defer srv.Close()
	cfg := Config{CoinbaseURL: srv.URL, IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03"}
	coin = "1.00000000"
	snap, err := fetchUSDTUSD(context.Background(), cfg)
	if err != nil || !snap.OK || snap.Rate.StringFixed(8) != "1.00000000" {
		t.Fatalf("a venue quote of 1 is data, not an assumed peg: %+v %v", snap, err)
	}
	coin = "0.5"
	if _, err := fetchUSDTUSD(context.Background(), cfg); err == nil {
		t.Fatal("out of band accepted")
	}
	cfg.CoinbaseURL = ""
	kr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":[],"result":{"USDTZUSD":{"c":["0.99910000","1"]}}}`))
	}))
	defer kr.Close()
	cfg.KrakenURL = kr.URL
	snap, err = fetchUSDTUSD(context.Background(), cfg)
	if err != nil || snap.Rate.StringFixed(8) != "0.99910000" {
		t.Fatalf("kraken: %+v %v", snap, err)
	}
}

func TestTickerVolumeFromTradeCandles(t *testing.T) {
	a := NewAggregator(nil, []string{"BTCUSDT"})
	now := a.now()
	ss := a.states["BTCUSDT"]
	ss.hasLast = true
	ss.lastPrice = decimal.NewFromInt(105)
	ss.series["1m"].candles = []Candle{{
		OpenTime: now.Add(-time.Hour).Unix(), Open: decimal.NewFromInt(100), High: decimal.NewFromInt(110),
		Low: decimal.NewFromInt(90), Close: decimal.NewFromInt(105), Volume: decimal.NewFromInt(2), QuoteVolume: decimal.NewFromInt(200),
	}}
	tk, ok := a.TickerFrame("BTCUSDT")
	if !ok || tk.Volume != "2" || tk.QuoteVolume != "200" {
		t.Fatalf("candle fallback %+v", tk)
	}
}
