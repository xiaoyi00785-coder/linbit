package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// fakeVenue serves market-data index, futures account/orders and Binance depth.
type fakeVenue struct {
	mu                         sync.Mutex
	resting                    int // rows returned by GET /futures/orders
	gets, cancels, places, ams int
	placedQty                  map[string]decimal.Decimal
}

func (f *fakeVenue) handler(t *testing.T) http.Handler {
	book, err := os.ReadFile("testdata/binance_fapi_btc_book.json")
	if err != nil {
		t.Fatal(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/market/index"):
			fmt.Fprint(w, `{"indexPrice":"61753.4","source":"test","widenFactor":"1"}`)
		case r.URL.Path == "/futures/positions":
			fmt.Fprint(w, `{"positions":[]}`)
		case r.URL.Path == "/futures/leverage":
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/fapi/v1/depth":
			w.Write(book)
		case r.URL.Path == "/futures/orders" && r.Method == "GET":
			f.gets++
			type o struct {
				OrderID int64  `json:"orderId"`
				Side    string `json:"side"`
				Price   string `json:"price"`
				Qty     string `json:"qty"`
				Filled  string `json:"filledQty"`
			}
			var os []o
			for i := 0; i < f.resting; i++ {
				side, px := "buy", 61000-float64(i)
				if i%2 == 1 {
					side, px = "sell", 62500+float64(i)
				}
				os = append(os, o{int64(i + 1), side, fmt.Sprint(px), "0.01", "0"})
			}
			json.NewEncoder(w).Encode(map[string]any{"orders": os})
		case r.URL.Path == "/futures/orders" && r.Method == "POST":
			var b struct{ Side, Qty string }
			json.NewDecoder(r.Body).Decode(&b)
			f.places++
			f.placedQty[b.Side] = f.placedQty[b.Side].Add(decimal.RequireFromString(b.Qty))
			fmt.Fprint(w, `{}`)
		case strings.HasPrefix(r.URL.Path, "/futures/orders/") && r.Method == "DELETE":
			f.cancels++
			fmt.Fprint(w, `{}`)
		case strings.HasPrefix(r.URL.Path, "/futures/orders/") && r.Method == "PUT":
			f.ams++
			http.Error(w, `{"code":"NOPE"}`, 400) // force amend fallback to cancel+place
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 404)
		}
	})
}

func newTestMaker(t *testing.T, f *fakeVenue) (*Maker, Rules) {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	cfg := Config{UserID: "mm:main", MarketURL: srv.URL, FuturesURL: srv.URL, BinanceURL: srv.URL, BinanceFAPIURL: srv.URL,
		BookFeed: "binance", BookScale: one, BookTTL: time.Millisecond, Workers: 4, PairConcurrency: 4, FuturesAmend: true,
		RefillMin: 300 * time.Millisecond, DepthMargin: d("10000000")}
	m := NewMaker(cfg, nil)
	m.books.client = http.DefaultClient
	m.alert = func(string) {}
	m.rules[key("futures", "BTCUSDT")] = btcRules
	return m, btcRules
}

func TestRunPairTruncationGuard(t *testing.T) {
	f := &fakeVenue{resting: 500, placedQty: map[string]decimal.Decimal{}}
	m, _ := newTestMaker(t, f)
	p := btcPair(200, 1, "60")
	p.Enabled, p.RefreshMs = true, 1000
	m.runPair(context.Background(), p, true, false, nil, false)
	st := m.state[key("futures", "BTCUSDT")]
	if f.places != 0 || f.cancels != 500 || st.Status != "error" || !strings.Contains(st.LastError, "truncated") {
		t.Fatalf("truncated listing must cancel all and place nothing: places=%d cancels=%d status=%s err=%q", f.places, f.cancels, st.Status, st.LastError)
	}
	// depth-off pair keeps the old behaviour (no guard): it diffs and places
	f2 := &fakeVenue{resting: 500, placedQty: map[string]decimal.Decimal{}}
	m2, _ := newTestMaker(t, f2)
	p2 := btcPair(100, 1, "0")
	p2.Enabled, p2.RefreshMs = true, 1000
	m2.runPair(context.Background(), p2, true, false, nil, false)
	if f2.places == 0 || m2.state[key("futures", "BTCUSDT")].Status != "quoting" {
		t.Fatalf("depth-off pair must behave as before: places=%d status=%s", f2.places, m2.state[key("futures", "BTCUSDT")].Status)
	}
}

func TestRunPairDepthQuotesAndRefill(t *testing.T) {
	f := &fakeVenue{placedQty: map[string]decimal.Decimal{}}
	m, _ := newTestMaker(t, f)
	p := btcPair(200, 1, "60")
	p.Enabled, p.RefreshMs = true, 1000
	ctx := context.Background()
	m.runPair(ctx, p, true, false, nil, false)
	st := m.state[key("futures", "BTCUSDT")]
	if st.Status != "quoting" || st.LastError != "" {
		t.Fatalf("status %s err %q", st.Status, st.LastError)
	}
	for _, side := range []string{"buy", "sell"} {
		if q := f.placedQty[side]; q.LessThan(d("59.9")) || q.GreaterThan(d("90")) {
			t.Fatalf("%s side placed %s BTC, want 60..90", side, q)
		}
	}
	if f.places > 440 {
		t.Fatalf("placed %d orders > 440", f.places)
	}
	// not due yet and book intact -> no re-quote
	f.resting, f.places = 0, 0
	st.LastRun = time.Now().Add(-400 * time.Millisecond)
	f.mu.Lock()
	f.resting = 0 // listing now empty: the whole book was taken
	f.mu.Unlock()
	m.runPair(ctx, p, true, false, nil, false)
	if st.Refills != 1 || f.places == 0 {
		t.Fatalf("swept book must refill before refresh_ms: refills=%d places=%d", st.Refills, f.places)
	}
	// depth-off pair: no extra open-orders call between refreshes
	f3 := &fakeVenue{placedQty: map[string]decimal.Decimal{}}
	m3, _ := newTestMaker(t, f3)
	p3 := btcPair(100, 1, "0")
	p3.Enabled, p3.RefreshMs = true, 3000
	m3.runPair(ctx, p3, true, false, nil, false)
	gets := f3.gets
	m3.state[key("futures", "BTCUSDT")].LastRun = time.Now().Add(-500 * time.Millisecond)
	m3.runPair(ctx, p3, true, false, nil, false)
	if f3.gets != gets || m3.state[key("futures", "BTCUSDT")].Refills != 0 {
		t.Fatalf("depth-off pair must not poll open orders between refreshes: gets %d -> %d", gets, f3.gets)
	}
}
