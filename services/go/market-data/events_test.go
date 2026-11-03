package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSink struct {
	trades []TradeEvent
	depths []DepthEvent
}

func (f *fakeSink) OnTrade(e TradeEvent) { f.trades = append(f.trades, e) }
func (f *fakeSink) OnDepth(e DepthEvent) { f.depths = append(f.depths, e) }

func newTestSource(sink Sink, token string) *HTTPEventSource {
	src := NewHTTPEventSource(sink, token)
	src.now = func() time.Time { return time.Unix(1762070400, 0) }
	return src
}

func TestDispatchTrade(t *testing.T) {
	fs := &fakeSink{}
	src := newTestSource(fs, "")
	src.dispatch(wireEvent{
		Event: "trade", TradeID: "1", Symbol: "BTCUSDT",
		Price: "70000", Quantity: "0.01", TakerSide: "buy",
	})
	if len(fs.trades) != 1 {
		t.Fatalf("trades = %d", len(fs.trades))
	}
	tr := fs.trades[0]
	if tr.Price.String() != "70000" || tr.Quantity.String() != "0.01" || tr.TakerSide != "buy" {
		t.Fatalf("trade = %+v", tr)
	}
	if tr.Time.Unix() != 1762070400 {
		t.Fatalf("trade time = %v", tr.Time)
	}
}

func TestDispatchDepth(t *testing.T) {
	fs := &fakeSink{}
	src := newTestSource(fs, "")
	src.dispatch(wireEvent{
		Event: "depth", Symbol: "BTCUSDT", Sequence: 7, Snapshot: false,
		Bids: []wireLevel{{Price: "70000", Quantity: "0.5"}},
		Asks: []wireLevel{{Price: "70010", Quantity: "0.3"}},
	})
	if len(fs.depths) != 1 {
		t.Fatalf("depths = %d", len(fs.depths))
	}
	d := fs.depths[0]
	if d.Sequence != 7 || len(d.Bids) != 1 || d.Bids[0].Price.String() != "70000" || d.Asks[0].Quantity.String() != "0.3" {
		t.Fatalf("depth = %+v", d)
	}
}

func TestIngestHandlerTokenAndDecode(t *testing.T) {
	fs := &fakeSink{}
	src := newTestSource(fs, "secret")
	h := src.Handler()

	// Wrong token -> 401, nothing delivered.
	req := httptest.NewRequest("POST", "/internal/events", strings.NewReader(`{"event":"trade"}`))
	req.Header.Set("X-Internal-Token", "nope")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad-token status = %d", w.Code)
	}

	// Correct token -> 200, trade delivered.
	body := `{"event":"trade","tradeId":"9","symbol":"BTCUSDT","price":"70000","quantity":"0.02","takerSide":"sell"}`
	req = httptest.NewRequest("POST", "/internal/events", strings.NewReader(body))
	req.Header.Set("X-Internal-Token", "secret")
	w = httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("good-token status = %d", w.Code)
	}
	if len(fs.trades) != 1 || fs.trades[0].TradeID != "9" {
		t.Fatalf("trades = %+v", fs.trades)
	}
}
