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

func coinbaseVenue(t *testing.T, products map[string]string, tickers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/ticker") {
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/products/"), "/ticker")
			if body, ok := tickers[id]; ok {
				w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.HasPrefix(path, "/products/") {
			id := strings.TrimPrefix(path, "/products/")
			if body, ok := products[id]; ok {
				w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func krakenVenue(t *testing.T, pairsBody, tickerBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/0/public/AssetPairs"):
			w.Write([]byte(pairsBody))
		case strings.HasPrefix(r.URL.Path, "/0/public/Ticker"):
			w.Write([]byte(tickerBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func onlineProduct(id string) string {
	return `{"id":"` + id + `","status":"online","trading_disabled":false,"cancel_only":false,"auction_mode":false}`
}

func TestUsdBaseAndRate(t *testing.T) {
	if b, ok := usdBase("TONUSDT"); !ok || b != "TON" {
		t.Fatalf("usdBase TONUSDT: %q %v", b, ok)
	}
	if _, ok := usdBase("USDT"); ok {
		t.Fatal("USDT alone must fail")
	}
	if _, ok := usdBase("TON-USD"); ok {
		t.Fatal("hyphenated must fail")
	}
	lo, hi := decimal.RequireFromString("0.97"), decimal.RequireFromString("1.03")
	if _, err := usdRate(decimal.RequireFromString("0.96"), lo, hi); err == nil {
		t.Fatal("below min must fail")
	}
	if _, err := usdRate(decimal.RequireFromString("1.04"), lo, hi); err == nil {
		t.Fatal("above max must fail")
	}
	if r, err := usdRate(decimal.RequireFromString("0.999"), lo, hi); err != nil || !r.Equal(decimal.RequireFromString("0.999")) {
		t.Fatalf("in-bounds: %v %v", r, err)
	}
	got := toUSDT(decimal.RequireFromString("5.0"), decimal.RequireFromString("0.5"))
	if !got.Equal(decimal.RequireFromString("10")) {
		t.Fatalf("toUSDT: %s", got)
	}
}

// Coinbase TON-USD + USDT-USD → TONUSDT; Binance TON is BREAK so failover lands on coinbase.
func TestCoinbaseTONIndex(t *testing.T) {
	bn := venue(t, map[string]string{
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING"},{"symbol":"TONUSDT","status":"BREAK"}]}`,
		"/api/v3/ticker/price": `[{"symbol":"BTCUSDT","price":"60000"},{"symbol":"TONUSDT","price":"1.6"}]`,
	})
	cb := coinbaseVenue(t,
		map[string]string{"USDT-USD": onlineProduct("USDT-USD"), "TON-USD": onlineProduct("TON-USD")},
		map[string]string{"USDT-USD": `{"price":"1.00"}`, "TON-USD": `{"price":"5.00"}`},
	)
	f := NewIndexFeed(Config{
		IndexFeed: []string{"binance", "coinbase"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, IndexUSDFallbackSymbols: []string{"TONUSDT"},
		IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03",
		BinanceURL: bn.URL, CoinbaseURL: cb.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	waitIndex(t, f, "BTCUSDT", "binance")
	r := waitIndex(t, f, "TONUSDT", "coinbase")
	if r.Price != "5" {
		t.Fatalf("TONUSDT want 5 (5/1), got %s", r.Price)
	}
}

// Missing / out-of-bounds USDT-USD rate fails the whole Coinbase venue (fail closed).
func TestCoinbaseBadUsdtUsdFailsVenue(t *testing.T) {
	cases := []struct {
		name  string
		price string
	}{
		{"missing", ""},
		{"too_low", "0.90"},
		{"too_high", "1.10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tickers := map[string]string{"TON-USD": `{"price":"5.00"}`}
			products := map[string]string{"TON-USD": onlineProduct("TON-USD")}
			if tc.price != "" {
				tickers["USDT-USD"] = `{"price":"` + tc.price + `"}`
				products["USDT-USD"] = onlineProduct("USDT-USD")
			} else {
				// USDT-USD product offline
				products["USDT-USD"] = `{"id":"USDT-USD","status":"delisted","trading_disabled":true,"cancel_only":false,"auction_mode":false}`
			}
			cb := coinbaseVenue(t, products, tickers)
			f := NewIndexFeed(Config{
				IndexFeed: []string{"coinbase"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
				IndexStatusRefresh: time.Minute, IndexUSDFallbackSymbols: []string{"TONUSDT"},
				IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03", CoinbaseURL: cb.URL,
			}, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.Start(ctx)
			time.Sleep(80 * time.Millisecond)
			if r, ok := f.Index("TONUSDT"); ok {
				t.Fatalf("bad USDT/USD must fail closed, got %+v", r)
			}
		})
	}
}

func TestKrakenTONIndex(t *testing.T) {
	kr := krakenVenue(t,
		`{"error":[],"result":{"USDTZUSD":{"altname":"USDTUSD","status":"online"},"TONUSD":{"altname":"TONUSD","status":"online"}}}`,
		`{"error":[],"result":{"USDTZUSD":{"c":["0.999","1"]},"TONUSD":{"c":["4.995","10"]}}}`,
	)
	f := NewIndexFeed(Config{
		IndexFeed: []string{"kraken"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, IndexUSDFallbackSymbols: []string{"TONUSDT"},
		IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03", KrakenURL: kr.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	r := waitIndex(t, f, "TONUSDT", "kraken")
	// 4.995 / 0.999 = 5
	want := decimal.RequireFromString("4.995").Div(decimal.RequireFromString("0.999"))
	got, err := decimal.NewFromString(r.Price)
	if err != nil || !got.Equal(want) {
		t.Fatalf("TONUSDT want %s, got %s", want, r.Price)
	}
}

// Unchanged-price guard watches raw USD: converted USDT price moving with the
// rate must not refresh changedAt when TON-USD is frozen.
func TestCoinbaseUnchangedWatchesRawUSD(t *testing.T) {
	var usdt atomic.Value
	usdt.Store("1.00")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/products/USDT-USD" || path == "/products/TON-USD":
			id := strings.TrimPrefix(path, "/products/")
			w.Write([]byte(onlineProduct(id)))
		case path == "/products/USDT-USD/ticker":
			w.Write([]byte(`{"price":"` + usdt.Load().(string) + `"}`))
		case path == "/products/TON-USD/ticker":
			w.Write([]byte(`{"price":"5.00"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	f := NewIndexFeed(Config{
		IndexFeed: []string{"coinbase"}, IndexPoll: 15 * time.Millisecond, IndexStale: time.Second,
		IndexUnchangedStale: 50 * time.Millisecond, IndexStatusRefresh: time.Minute,
		IndexUSDFallbackSymbols: []string{"TONUSDT"},
		IndexUsdtUsdMin:         "0.97", IndexUsdtUsdMax: "1.03", CoinbaseURL: srv.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	waitIndex(t, f, "TONUSDT", "coinbase")

	var src *indexSource
	for _, s := range f.sources {
		if s.name == "coinbase" {
			src = s
			break
		}
	}
	if src == nil {
		t.Fatal("no coinbase source")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	var firstChanged time.Time
	for time.Now().Before(deadline) {
		src.mu.Lock()
		q, ok := src.quotes["TONUSDT"]
		src.mu.Unlock()
		if ok && !q.changedAt.IsZero() {
			firstChanged = q.changedAt
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if firstChanged.IsZero() {
		t.Fatal("no initial quote")
	}
	// Move only the USDT/USD rate; raw TON-USD stays 5.00.
	usdt.Store("0.98")
	time.Sleep(120 * time.Millisecond)
	src.mu.Lock()
	q := src.quotes["TONUSDT"]
	src.mu.Unlock()
	if !q.changedAt.Equal(firstChanged) {
		t.Fatalf("raw USD frozen: changedAt moved from %v to %v (price now %s)", firstChanged, q.changedAt, q.price)
	}
	// And the quote must eventually be considered not fresh by the unchanged guard.
	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := f.Index("TONUSDT"); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("frozen raw USD must make index unusable after unchangedStale")
}

func TestUSDSourcesOnlyQuoteFallbackSymbols(t *testing.T) {
	cb := coinbaseVenue(t,
		map[string]string{
			"USDT-USD": onlineProduct("USDT-USD"),
			"TON-USD":  onlineProduct("TON-USD"),
			"BTC-USD":  onlineProduct("BTC-USD"),
		},
		map[string]string{
			"USDT-USD": `{"price":"1.00"}`,
			"TON-USD":  `{"price":"5.00"}`,
			"BTC-USD":  `{"price":"60000"}`,
		},
	)
	f := NewIndexFeed(Config{
		IndexFeed: []string{"coinbase"}, IndexPoll: 10 * time.Millisecond, IndexStale: time.Second,
		IndexStatusRefresh: time.Minute, IndexUSDFallbackSymbols: []string{"TONUSDT"},
		IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03", CoinbaseURL: cb.URL,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)
	waitIndex(t, f, "TONUSDT", "coinbase")
	time.Sleep(50 * time.Millisecond)
	if r, ok := f.Index("BTCUSDT"); ok {
		t.Fatalf("BTCUSDT must not come from USD venue, got %+v", r)
	}
}

func TestParseFeedIncludesCoinbaseKraken(t *testing.T) {
	got := parseFeed("binance,okx,bybit,gateio,coinbase,kraken")
	want := []string{"binance", "okx", "bybit", "gateio", "coinbase", "kraken"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}
