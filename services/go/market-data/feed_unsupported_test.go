package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// Live mainnet (HK egress) measured: Bybit spot TONUSDT → HTTP 200 +
// retCode=10001 "Not supported symbols"; Gate.io TON_USDT → HTTP 400
// INVALID_CURRENCY (TON delisted). These tests pin the fail-closed /
// per-symbol failover behaviour that production depends on.

func TestBybitRetCodeNonZeroFailsSourceNoZeroPrice(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.HasPrefix(r.URL.Path, "/v5/market/instruments-info"):
			// Venue answers HTTP 200 with an application-level error for the
			// unsupported symbol path (and for a broken instruments call).
			w.Write([]byte(`{"retCode":10001,"retMsg":"Not supported symbols","result":{"list":[]}}`))
		case strings.HasPrefix(r.URL.Path, "/v5/market/tickers"):
			// Even if tickers somehow answered with a zero price, retCode!=0
			// on instruments must keep the whole Bybit source from quoting.
			w.Write([]byte(`{"retCode":10001,"retMsg":"Not supported symbols","result":{"list":[{"symbol":"TONUSDT","lastPrice":"0"}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	st := newStatusCache(time.Minute, bybitTrading(srv.URL))
	st.inline = true
	fetch := withStatus(func(ctx context.Context) (map[string]decimal.Decimal, error) {
		return fetchBybit(ctx, srv.URL)
	}, st)
	prices, err := fetch(ctx)
	if err == nil {
		t.Fatalf("retCode!=0 must fail the Bybit source, got prices=%v", prices)
	}
	if !strings.Contains(err.Error(), "10001") && !strings.Contains(err.Error(), "retCode") && !strings.Contains(err.Error(), "instrument") {
		t.Fatalf("error should mention retCode/instrument failure, got %v", err)
	}
	if prices != nil {
		t.Fatalf("must not return a price map on failure (got %v)", prices)
	}

	// Direct ticker call with retCode!=0 (instruments already loaded empty path
	// is covered above): never invent a zero quote.
	p, err := fetchBybit(ctx, srv.URL)
	if err == nil || p != nil {
		t.Fatalf("fetchBybit retCode!=0: prices=%v err=%v", p, err)
	}
	if !strings.Contains(err.Error(), "10001") {
		t.Fatalf("want retCode 10001 in error, got %v", err)
	}
}

func TestGateHTTP400FailsSourceNoZeroPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v4/spot/currency_pairs"):
			// List endpoint works for other pairs; TON is simply absent (delisted).
			w.Write([]byte(`[{"id":"BTC_USDT","trade_status":"tradable"}]`))
		case strings.HasPrefix(r.URL.Path, "/api/v4/spot/tickers"):
			// Live observation for TON_USDT specifically: HTTP 400 INVALID_CURRENCY.
			// If the all-tickers call ever returns 400 the whole Gate source must
			// fail closed (no partial zero-price map).
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"label":"INVALID_CURRENCY","message":"currency TON is delisted"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	st := newStatusCache(time.Minute, gateTrading(srv.URL))
	st.inline = true
	fetch := withStatus(func(ctx context.Context) (map[string]decimal.Decimal, error) {
		return fetchGate(ctx, srv.URL)
	}, st)
	prices, err := fetch(ctx)
	if err == nil {
		t.Fatalf("Gate HTTP 400 must fail the source, got prices=%v", prices)
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("want HTTP 400 in error, got %v", err)
	}
	if prices != nil {
		t.Fatalf("must not return a price map on failure (got %v)", prices)
	}

	p, err := fetchGate(ctx, srv.URL)
	if err == nil || p != nil {
		t.Fatalf("fetchGate HTTP 400: prices=%v err=%v", p, err)
	}
}

func TestBybitSkipsZeroLastPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[
			{"symbol":"TONUSDT","lastPrice":"0"},
			{"symbol":"BTCUSDT","lastPrice":"60000"}
		]}}`))
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	p, err := fetchBybit(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p["TONUSDT"]; ok {
		t.Fatalf("zero lastPrice must be omitted, got %v", p)
	}
	if p["BTCUSDT"].String() != "60000" {
		t.Fatalf("BTCUSDT=%s", p["BTCUSDT"])
	}
}

// Per-symbol failover: Binance TON=BREAK, OKX has no TON, Bybit retCode=10001
// (unsupported), Gate tickers HTTP 400 (delisted) — TON lands on Coinbase;
// BTC keeps using Binance and is unaffected by the TON-side venue errors.
func TestTONFallsThroughToCoinbaseWhileBTCStaysOnBinance(t *testing.T) {
	bn := venue(t, map[string]string{
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING"},{"symbol":"TONUSDT","status":"BREAK"}]}`,
		"/api/v3/ticker/price": `[{"symbol":"BTCUSDT","price":"60000"},{"symbol":"TONUSDT","price":"1.6"}]`,
	})
	okx := venue(t, map[string]string{
		"/api/v5/public/instruments": `{"code":"0","data":[{"instId":"BTC-USDT","state":"live"}]}`,
		"/api/v5/market/tickers":     `{"code":"0","data":[{"instId":"BTC-USDT","last":"60001"}]}`,
	})
	by := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mimic live: HTTP 200 + retCode 10001 for unsupported TON (and we treat
		// that as the venue failing this poll — never a TON quote).
		w.Write([]byte(`{"retCode":10001,"retMsg":"Not supported symbols","result":{"list":[]}}`))
	}))
	t.Cleanup(by.Close)
	ga := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v4/spot/currency_pairs") {
			w.Write([]byte(`[{"id":"BTC_USDT","trade_status":"tradable"}]`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"label":"INVALID_CURRENCY","message":"currency TON is delisted"}`))
	}))
	t.Cleanup(ga.Close)
	cb := coinbaseVenue(t,
		map[string]string{"USDT-USD": onlineProduct("USDT-USD"), "TON-USD": onlineProduct("TON-USD")},
		map[string]string{"USDT-USD": `{"price":"1.00"}`, "TON-USD": `{"price":"5.00"}`},
	)

	f := NewIndexFeed(Config{
		IndexFeed: []string{"binance", "okx", "bybit", "gateio", "coinbase", "kraken"},
		IndexPoll: 15 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, IndexUSDFallbackSymbols: []string{"TONUSDT"},
		IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03",
		BinanceURL: bn.URL, OKXURL: okx.URL, BybitURL: by.URL, GateURL: ga.URL, CoinbaseURL: cb.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)

	btc := waitIndex(t, f, "BTCUSDT", "binance")
	if btc.Price != "60000" {
		t.Fatalf("BTCUSDT want 60000 from binance, got %+v", btc)
	}
	ton := waitIndex(t, f, "TONUSDT", "coinbase")
	if ton.Price != "5" {
		t.Fatalf("TONUSDT want 5 from coinbase, got %+v", ton)
	}
	// Never a zero-price index for the unsupported symbol on any component.
	for _, c := range f.Components("TONUSDT", ton.Source) {
		if c.Price == "0" || c.Price == "0.0" {
			t.Fatalf("component must not expose price 0: %+v", c)
		}
		if c.Exchange == "bybit" && c.Fresh {
			t.Fatalf("bybit must not be fresh for unsupported TON: %+v", c)
		}
		if c.Exchange == "gateio" && c.Fresh {
			t.Fatalf("gateio must not be fresh for delisted TON: %+v", c)
		}
	}
}

// A permanently failing Bybit must not log on every poll (rate-limited to the
// transition into the failing state). Other symbols keep updating.
func TestPermanentBybitFailureDoesNotSpamLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	bn := venue(t, map[string]string{
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING"}]}`,
		"/api/v3/ticker/price": `[{"symbol":"BTCUSDT","price":"60000"}]`,
	})
	by := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"retCode":10001,"retMsg":"Not supported symbols","result":{"list":[]}}`))
	}))
	t.Cleanup(by.Close)

	f := NewIndexFeed(Config{
		IndexFeed: []string{"binance", "bybit"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, BinanceURL: bn.URL, BybitURL: by.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	waitIndex(t, f, "BTCUSDT", "binance")
	time.Sleep(200 * time.Millisecond) // many Bybit poll failures

	logs := buf.String()
	n := strings.Count(logs, "index feed bybit:")
	if n == 0 {
		t.Fatalf("expected one failure log, got none; logs=%q", logs)
	}
	if n > 2 {
		t.Fatalf("bybit failure must be rate-limited (got %d lines); logs=%q", n, logs)
	}
	// BTC still healthy after the Bybit outage window.
	if r, ok := f.Index("BTCUSDT"); !ok || r.Source != "binance" {
		t.Fatalf("BTCUSDT must stay on binance, got %+v ok=%v", r, ok)
	}
}

// When Bybit/Gate simply omit TON (no error), there is no per-tick log spam
// either — silent skip, Coinbase serves TON, Binance serves BTC.
func TestUnsupportedTONOmittedSilentlyNoSpam(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	bn := venue(t, map[string]string{
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING"},{"symbol":"TONUSDT","status":"BREAK"}]}`,
		"/api/v3/ticker/price": `[{"symbol":"BTCUSDT","price":"60000"},{"symbol":"TONUSDT","price":"1.6"}]`,
	})
	by := venue(t, map[string]string{
		"/v5/market/instruments-info": `{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","status":"Trading"}],"nextPageCursor":""}}`,
		"/v5/market/tickers":          `{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","lastPrice":"60002"}]}}`,
	})
	ga := venue(t, map[string]string{
		"/api/v4/spot/currency_pairs": `[{"id":"BTC_USDT","trade_status":"tradable"}]`,
		"/api/v4/spot/tickers":        `[{"currency_pair":"BTC_USDT","last":"60003"}]`,
	})
	cb := coinbaseVenue(t,
		map[string]string{"USDT-USD": onlineProduct("USDT-USD"), "TON-USD": onlineProduct("TON-USD")},
		map[string]string{"USDT-USD": `{"price":"1.00"}`, "TON-USD": `{"price":"5.00"}`},
	)
	f := NewIndexFeed(Config{
		IndexFeed: []string{"binance", "bybit", "gateio", "coinbase"},
		IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, IndexUSDFallbackSymbols: []string{"TONUSDT"},
		IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03",
		BinanceURL: bn.URL, BybitURL: by.URL, GateURL: ga.URL, CoinbaseURL: cb.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	waitIndex(t, f, "BTCUSDT", "binance")
	waitIndex(t, f, "TONUSDT", "coinbase")
	time.Sleep(150 * time.Millisecond)
	logs := buf.String()
	for _, needle := range []string{"index feed bybit:", "index feed gateio:", "index feed coinbase:"} {
		if strings.Contains(logs, needle) {
			t.Fatalf("healthy omit-TON path must not log source errors (%s); logs=%q", needle, logs)
		}
	}
}

