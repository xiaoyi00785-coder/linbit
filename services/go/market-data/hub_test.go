package main

import (
	"encoding/json"
	"testing"
	"time"
)

type fakeSnap struct{}

func (fakeSnap) HasSymbol(s string) bool     { return s == "BTCUSDT" }
func (fakeSnap) ValidInterval(n string) bool { return n == "1m" }
func (fakeSnap) DepthFrame(s string, _ int) (depthDTO, bool) {
	return depthDTO{Symbol: s, Bids: [][2]string{}, Asks: [][2]string{}}, true
}
func (fakeSnap) TickerFrame(s string) (tickerDTO, bool) {
	return tickerDTO{Symbol: s, LastPrice: "1"}, true
}
func (fakeSnap) KlineFrame(_, _ string) (wsKline, bool) { return wsKline{}, false }

func newTestClient(h *Hub) *wsClient {
	return &wsClient{
		hub:    h,
		send:   make(chan outFrame, 16),
		subs:   map[string]struct{}{},
		closed: make(chan struct{}),
	}
}

func nextFrame(t *testing.T, c *wsClient) []byte {
	t.Helper()
	select {
	case f := <-c.send:
		return f.data
	case <-time.After(time.Second):
		t.Fatal("expected a frame, got none")
		return nil
	}
}

func TestHubBroadcastRouting(t *testing.T) {
	h := NewHub(fakeSnap{})
	c := newTestClient(h)
	h.subscribe(c, "ticker:BTCUSDT")
	c.subs["ticker:BTCUSDT"] = struct{}{}

	if h.channelCount("ticker:BTCUSDT") != 1 {
		t.Fatalf("subscriber count = %d", h.channelCount("ticker:BTCUSDT"))
	}

	h.Broadcast("ticker:BTCUSDT", tickerDTO{Symbol: "BTCUSDT", LastPrice: "70000"})
	var frame wsData
	if err := json.Unmarshal(nextFrame(t, c), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if frame.Channel != "ticker:BTCUSDT" || frame.TS == 0 {
		t.Fatalf("frame = %+v", frame)
	}

	// Broadcasting on a channel nobody is subscribed to delivers nothing.
	h.Broadcast("depth:BTCUSDT", depthDTO{})
	select {
	case <-c.send:
		t.Fatal("unexpected frame on unsubscribed channel")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubDoSubscribeSendsReceiptThenSnapshot(t *testing.T) {
	h := NewHub(fakeSnap{})
	c := newTestClient(h)
	c.doSubscribe("req1", "ticker:BTCUSDT")

	var receipt wsReceipt
	if err := json.Unmarshal(nextFrame(t, c), &receipt); err != nil {
		t.Fatalf("receipt unmarshal: %v", err)
	}
	if receipt.Event != "subscribed" || receipt.Channel != "ticker:BTCUSDT" || receipt.ID != "req1" {
		t.Fatalf("receipt = %+v", receipt)
	}

	var snap wsData
	if err := json.Unmarshal(nextFrame(t, c), &snap); err != nil {
		t.Fatalf("snapshot unmarshal: %v", err)
	}
	if snap.Channel != "ticker:BTCUSDT" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestHubValidateChannel(t *testing.T) {
	h := NewHub(fakeSnap{})
	c := newTestClient(h)
	valid := []string{"ticker:BTCUSDT", "depth:BTCUSDT", "trades:BTCUSDT", "kline:BTCUSDT:1m"}
	for _, ch := range valid {
		if code, _ := c.validateChannel(ch); code != "" {
			t.Fatalf("channel %s rejected: %s", ch, code)
		}
	}
	invalid := []string{"ticker:FOO", "kline:BTCUSDT:7m", "orders", "balances", "bogus:BTCUSDT", "kline:BTCUSDT"}
	for _, ch := range invalid {
		if code, _ := c.validateChannel(ch); code == "" {
			t.Fatalf("channel %s unexpectedly accepted", ch)
		}
	}
}

func TestHubHandlePing(t *testing.T) {
	h := NewHub(fakeSnap{})
	c := newTestClient(h)
	c.handle([]byte(`{"op":"ping"}`))
	var msg map[string]any
	if err := json.Unmarshal(nextFrame(t, c), &msg); err != nil {
		t.Fatalf("pong unmarshal: %v", err)
	}
	if msg["event"] != "pong" {
		t.Fatalf("expected pong, got %+v", msg)
	}
}

func TestHubUnsubscribe(t *testing.T) {
	h := NewHub(fakeSnap{})
	c := newTestClient(h)
	c.doSubscribe("1", "ticker:BTCUSDT")
	// drain receipt + snapshot
	_ = nextFrame(t, c)
	_ = nextFrame(t, c)

	c.handle([]byte(`{"id":"2","op":"unsubscribe","args":["ticker:BTCUSDT"]}`))
	if h.channelCount("ticker:BTCUSDT") != 0 {
		t.Fatalf("still subscribed after unsubscribe")
	}
	var receipt wsReceipt
	if err := json.Unmarshal(nextFrame(t, c), &receipt); err != nil {
		t.Fatalf("unsub receipt: %v", err)
	}
	if receipt.Event != "unsubscribed" {
		t.Fatalf("receipt = %+v", receipt)
	}
}
