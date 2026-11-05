package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// venue is a fake exchange serving both ticker and instrument-status endpoints.
func venue(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for prefix, body := range routes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				w.Write([]byte(body))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitIndex(t *testing.T, f *IndexFeed, sym string, want string) IndexResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var r IndexResult
	var ok bool
	for time.Now().Before(deadline) {
		r, ok = f.Index(sym)
		if (want == "" && !ok) || (ok && r.Source == want) {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: want source %q, got %+v ok=%v", sym, want, r, ok)
	return r
}

// Binance still quotes TONUSDT while the pair is BREAK; OKX has no TON-USDT.
// Without a trading source the index must be unavailable (fresh:false).
func TestHaltedBinanceSymbolIsDropped(t *testing.T) {
	bn := venue(t, map[string]string{
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING"},{"symbol":"TONUSDT","status":"BREAK"}]}`,
		"/api/v3/ticker/price": `[{"symbol":"BTCUSDT","price":"60000"},{"symbol":"TONUSDT","price":"1.6"}]`,
	})
	ok := venue(t, map[string]string{
		"/api/v5/public/instruments": `{"code":"0","data":[{"instId":"BTC-USDT","state":"live"},{"instId":"OLD-USDT","state":"suspend"}]}`,
		"/api/v5/market/tickers":     `{"code":"0","data":[{"instId":"BTC-USDT","last":"60001"},{"instId":"OLD-USDT","last":"5"}]}`,
	})
	f := NewIndexFeed(Config{IndexFeed: []string{"binance", "okx"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, BinanceURL: bn.URL, OKXURL: ok.URL}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	waitIndex(t, f, "BTCUSDT", "binance")
	time.Sleep(100 * time.Millisecond) // several polls
	if r, ok := f.Index("TONUSDT"); ok {
		t.Fatalf("halted TONUSDT must not have an index, got %+v", r)
	}
	if r, ok := f.Index("OLDUSDT"); ok {
		t.Fatalf("suspended OKX instrument must not have an index, got %+v", r)
	}
	for _, c := range f.Components("TONUSDT", "") {
		if c.Fresh || c.Price != "" {
			t.Fatalf("component %+v should be absent/not fresh", c)
		}
	}
}

// TON is halted on Binance and missing on OKX but trading on Bybit and Gate:
// the index falls through to Bybit, then Gate when Bybit also halts it.
func TestFailoverToBybitAndGate(t *testing.T) {
	var bybitStatus atomic.Value
	bybitStatus.Store("Trading")
	bn := venue(t, map[string]string{
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"TONUSDT","status":"BREAK"}]}`,
		"/api/v3/ticker/price": `[{"symbol":"TONUSDT","price":"1.6"}]`,
	})
	ok := venue(t, map[string]string{
		"/api/v5/public/instruments": `{"code":"0","data":[{"instId":"BTC-USDT","state":"live"}]}`,
		"/api/v5/market/tickers":     `{"code":"0","data":[{"instId":"BTC-USDT","last":"60001"}]}`,
	})
	by := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/market/instruments-info":
			w.Write([]byte(`{"retCode":0,"result":{"list":[{"symbol":"TONUSDT","status":"` + bybitStatus.Load().(string) + `"},{"symbol":"BTCUSDT","status":"Trading"}],"nextPageCursor":""}}`))
		case "/v5/market/tickers":
			w.Write([]byte(`{"retCode":0,"result":{"list":[{"symbol":"TONUSDT","lastPrice":"1.17"},{"symbol":"BTCUSDT","lastPrice":"60002"}]}}`))
		}
	}))
	defer by.Close()
	ga := venue(t, map[string]string{
		"/api/v4/spot/currency_pairs": `[{"id":"TON_USDT","trade_status":"tradable"},{"id":"XYZ_USDT","trade_status":"untradable"}]`,
		"/api/v4/spot/tickers":        `[{"currency_pair":"TON_USDT","last":"1.18"},{"currency_pair":"XYZ_USDT","last":"9"}]`,
	})
	// short status refresh so the Bybit halt is noticed; sync refresh keeps the test deterministic
	f := NewIndexFeed(Config{IndexFeed: []string{"binance", "okx", "bybit", "gateio"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: 50 * time.Millisecond, BinanceURL: bn.URL, OKXURL: ok.URL, BybitURL: by.URL, GateURL: ga.URL}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	if r := waitIndex(t, f, "TONUSDT", "bybit"); r.Price != "1.17" {
		t.Fatalf("price %s", r.Price)
	}
	if r := waitIndex(t, f, "BTCUSDT", "okx"); r.Price != "60001" {
		t.Fatalf("price %s", r.Price)
	}
	if _, ok := f.Index("XYZUSDT"); ok {
		t.Fatal("untradable Gate pair must not be quoted")
	}
	bybitStatus.Store("Delisted")
	if r := waitIndex(t, f, "TONUSDT", "gateio"); r.Price != "1.18" {
		t.Fatalf("price %s", r.Price)
	}
}

func TestStatusCacheFailClosed(t *testing.T) {
	var fail atomic.Bool
	calls := 0
	c := newStatusCache(time.Minute, func(ctx context.Context) (map[string]bool, error) {
		calls++
		if fail.Load() {
			return nil, errors.New("boom")
		}
		return map[string]bool{"BTCUSDT": true}, nil
	})
	now := time.Now()
	c.now = func() time.Time { return now }
	c.inline = true
	fail.Store(true)
	if _, err := c.trading(); err == nil {
		t.Fatal("no status yet and load failing must fail closed")
	}
	fail.Store(false)
	if set, err := c.trading(); err != nil || !set["BTCUSDT"] {
		t.Fatalf("%v %v", set, err)
	}
	n := calls
	if _, err := c.trading(); err != nil || calls != n {
		t.Fatalf("cache hit expected (calls %d->%d, err %v)", n, calls, err)
	}
	// refresh fails: the last good set keeps serving until maxAge, then fail closed
	fail.Store(true)
	now = now.Add(2 * time.Minute)
	if set, err := c.trading(); err != nil || !set["BTCUSDT"] {
		t.Fatalf("last good set expected: %v %v", set, err)
	}
	now = now.Add(5 * time.Minute)
	if _, err := c.trading(); err == nil {
		t.Fatal("status older than maxAge must fail closed")
	}
}

func TestParseBybitAndGate(t *testing.T) {
	by := venue(t, map[string]string{"/v5/market/tickers": `{"retCode":0,"result":{"list":[{"symbol":"TONUSDT","lastPrice":"1.17"},{"symbol":"BADUSDT","lastPrice":""}]}}`})
	got, err := fetchBybit(context.Background(), by.URL)
	if err != nil || len(got) != 1 || !got["TONUSDT"].Equal(decimal.RequireFromString("1.17")) {
		t.Fatalf("%v %v", got, err)
	}
	byErr := venue(t, map[string]string{"/v5/market/tickers": `{"retCode":10001,"retMsg":"Not supported symbols","result":{}}`})
	if _, err := fetchBybit(context.Background(), byErr.URL); err == nil {
		t.Fatal("non-zero retCode must error")
	}
	ga := venue(t, map[string]string{"/api/v4/spot/tickers": `[{"currency_pair":"TON_USDT","last":"1.18"},{"currency_pair":"X_USDT","last":"0"}]`})
	got, err = fetchGate(context.Background(), ga.URL)
	if err != nil || !got["TONUSDT"].Equal(decimal.RequireFromString("1.18")) {
		t.Fatalf("%v %v", got, err)
	}
	tr, err := gateTrading(venue(t, map[string]string{"/api/v4/spot/currency_pairs": `[{"id":"BTC_USDT","trade_status":"tradable"},{"id":"OLD_USDT","trade_status":"untradable"}]`}).URL)(context.Background())
	if err != nil || !tr["BTCUSDT"] || tr["OLDUSDT"] {
		t.Fatalf("%v %v", tr, err)
	}
}

// A venue that keeps serving the same frozen price is not fresh after
// IndexUnchangedStale, except for exempt (pegged) symbols.
func TestUnchangedPriceGuard(t *testing.T) {
	now := time.Now()
	f := NewIndexFeed(Config{IndexFeed: []string{"binance", "okx"}, IndexStale: 10 * time.Second,
		IndexUnchangedStale: 30 * time.Minute, IndexUnchangedExempt: []string{"usdcusdt"}}, nil)
	q := func(p string, changed time.Duration) indexQuote {
		return indexQuote{price: decimal.RequireFromString(p), at: now, changedAt: now.Add(-changed)}
	}
	f.sources[0].quotes = map[string]indexQuote{"TONUSDT": q("1.6", 31*time.Minute), "BTCUSDT": q("60000", 29*time.Minute), "USDCUSDT": q("1.0000", 5*time.Hour)}
	f.sources[1].quotes = map[string]indexQuote{"BTCUSDT": q("60001", time.Minute), "TONUSDT": q("1.2", time.Minute)}
	if r, ok := f.Index("TONUSDT"); !ok || r.Source != "okx" {
		t.Fatalf("frozen binance TON must fail over to okx: %+v %v", r, ok)
	}
	if r, ok := f.Index("BTCUSDT"); !ok || r.Source != "binance" {
		t.Fatalf("29m-old change is still fresh: %+v %v", r, ok)
	}
	if r, ok := f.Index("USDCUSDT"); !ok || r.Source != "binance" {
		t.Fatalf("exempt pegged pair stays fresh: %+v %v", r, ok)
	}
	cs := f.Components("TONUSDT", "okx")
	if cs[0].Fresh || !cs[1].Fresh {
		t.Fatalf("components %+v", cs)
	}
	// both frozen: unavailable, and the 503 body says fresh:false
	f.sources[1].quotes["TONUSDT"] = q("1.2", time.Hour)
	if _, ok := f.Index("TONUSDT"); ok {
		t.Fatal("every source frozen: index must be unavailable")
	}
	srv := &Server{feed: f}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/market/index?symbol=TONUSDT", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"fresh":false`) || !strings.Contains(rec.Body.String(), "INDEX_UNAVAILABLE") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	// guard off
	f.unchangedStale = 0
	if _, ok := f.Index("TONUSDT"); !ok {
		t.Fatal("guard disabled")
	}
}

// A symbol the venue stops quoting is removed from the source's quotes.
func TestRunDropsVanishedSymbols(t *testing.T) {
	var body atomic.Value
	body.Store(`[{"symbol":"AAAUSDT","price":"1"},{"symbol":"BBBUSDT","price":"2"}]`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body.Load().(string))) }))
	defer srv.Close()
	src := &indexSource{name: "x", quotes: map[string]indexQuote{}, fetch: func(ctx context.Context) (map[string]decimal.Decimal, error) {
		return fetchBinance(ctx, srv.URL)
	}}
	f := &IndexFeed{poll: 10 * time.Millisecond, stale: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.run(ctx, src)
	has := func(sym string) bool { src.mu.Lock(); defer src.mu.Unlock(); _, ok := src.quotes[sym]; return ok }
	deadline := time.Now().Add(2 * time.Second)
	for !has("BBBUSDT") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	body.Store(`[{"symbol":"AAAUSDT","price":"1"}]`)
	for has("BBBUSDT") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if has("BBBUSDT") || !has("AAAUSDT") {
		t.Fatal("vanished symbol must be dropped, others kept")
	}
	time.Sleep(60 * time.Millisecond)
	src.mu.Lock()
	first := src.quotes["AAAUSDT"].changedAt
	at := src.quotes["AAAUSDT"].at
	src.mu.Unlock()
	if !at.After(first) {
		t.Fatalf("unchanged price must keep changedAt (changed %v, at %v)", first, at)
	}
}
