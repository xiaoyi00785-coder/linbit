package main

import (
	"github.com/shopspring/decimal"
	"strings"
	"sync"
	"testing"
	"time"
)

// recBroadcaster records every broadcast for assertions.
type recBroadcaster struct {
	mu     sync.Mutex
	frames []recFrame
}

type recFrame struct {
	channel string
	data    any
}

func (r *recBroadcaster) Broadcast(channel string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, recFrame{channel: channel, data: data})
}

func (r *recBroadcaster) count(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, f := range r.frames {
		if strings.HasPrefix(f.channel, prefix) {
			n++
		}
	}
	return n
}

func newTestAgg() (*Aggregator, *recBroadcaster) {
	agg := NewAggregator(nil, []string{"BTCUSDT"})
	bc := &recBroadcaster{}
	agg.SetBroadcaster(bc)
	// Fix "now" so the 24h ticker window never expires mid-test.
	agg.now = func() time.Time { return time.Unix(1762070500, 0) }
	return agg, bc
}

func trade(id, price, qty, side string, ts int64) TradeEvent {
	return TradeEvent{
		TradeID:   id,
		Symbol:    "BTCUSDT",
		Price:     dec(price),
		Quantity:  dec(qty),
		TakerSide: side,
		Time:      time.Unix(ts, 0),
	}
}

func TestOnTradeBuildsKlineTickerTrades(t *testing.T) {
	agg, bc := newTestAgg()
	base := int64(1762070400) // minute-aligned

	agg.OnTrade(trade("1", "70000", "0.01", "buy", base))

	// Kline (1m).
	ks, ok := agg.Klines("BTCUSDT", "1m", 10)
	if !ok || len(ks) != 1 {
		t.Fatalf("klines = %v ok=%v", ks, ok)
	}
	if ks[0].Time != base || ks[0].Close != "70000" || ks[0].Volume != "0.01" {
		t.Fatalf("kline = %+v", ks[0])
	}

	// Recent trades (newest first).
	tr, _ := agg.RecentTrades("BTCUSDT", 10)
	if len(tr) != 1 || tr[0].TradeID != "1" || tr[0].Price != "70000" || tr[0].Side != "buy" {
		t.Fatalf("recent trades = %+v", tr)
	}

	// Ticker.
	tk, _ := agg.TickerFrame("BTCUSDT")
	if tk.LastPrice != "70000" || tk.Volume != "0.01" || tk.QuoteVolume != "700" || tk.PriceChangePercent != "0" {
		t.Fatalf("ticker = %+v", tk)
	}

	// Broadcasts fired for each public channel.
	if bc.count("trades:BTCUSDT") != 1 || bc.count("ticker:BTCUSDT") != 1 || bc.count("kline:BTCUSDT:1m") != 1 {
		t.Fatalf("broadcast counts: trades=%d ticker=%d kline1m=%d",
			bc.count("trades:BTCUSDT"), bc.count("ticker:BTCUSDT"), bc.count("kline:BTCUSDT:1m"))
	}
}

func TestOnTradeAggregatesWithinBucket(t *testing.T) {
	agg, _ := newTestAgg()
	base := int64(1762070400)

	agg.OnTrade(trade("1", "70000", "0.01", "buy", base))
	agg.OnTrade(trade("2", "70500", "0.02", "sell", base+10))

	ks, _ := agg.Klines("BTCUSDT", "1m", 10)
	if len(ks) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(ks))
	}
	c := ks[0]
	if c.High != "70500" || c.Low != "70000" || c.Close != "70500" || c.Volume != "0.03" {
		t.Fatalf("aggregated candle = %+v", c)
	}

	tk, _ := agg.TickerFrame("BTCUSDT")
	// 24h stats: high 70500, low 70000, vol 0.03, quoteVol 700+1410=2110.
	if tk.High != "70500" || tk.Low != "70000" || tk.Volume != "0.03" || tk.QuoteVolume != "2110" {
		t.Fatalf("ticker = %+v", tk)
	}
	// pct = (70500-70000)/70000*100 = 0.714... -> 0.71
	if tk.PriceChangePercent != "0.71" {
		t.Fatalf("priceChangePercent = %s, want 0.71", tk.PriceChangePercent)
	}
}

func TestOnTradeRollsBucket(t *testing.T) {
	agg, _ := newTestAgg()
	base := int64(1762070400)

	agg.OnTrade(trade("1", "70000", "0.01", "buy", base))
	agg.OnTrade(trade("2", "71000", "0.01", "buy", base+60)) // next minute

	ks, _ := agg.Klines("BTCUSDT", "1m", 10)
	if len(ks) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(ks))
	}
	if ks[0].Time != base || ks[1].Time != base+60 {
		t.Fatalf("candle times = %d, %d", ks[0].Time, ks[1].Time)
	}
	if ks[1].Open != "71000" || ks[1].Close != "71000" {
		t.Fatalf("second candle = %+v", ks[1])
	}
}

func TestOnTradeDedupByTradeID(t *testing.T) {
	agg, _ := newTestAgg()
	base := int64(1762070400)
	agg.OnTrade(trade("5", "70000", "0.01", "buy", base))
	agg.OnTrade(trade("5", "70000", "0.01", "buy", base)) // duplicate id
	agg.OnTrade(trade("4", "70000", "0.01", "buy", base)) // stale id

	tr, _ := agg.RecentTrades("BTCUSDT", 10)
	if len(tr) != 1 {
		t.Fatalf("expected 1 trade after dedup, got %d", len(tr))
	}
}

func TestOnDepthSnapshotAndDelta(t *testing.T) {
	agg, bc := newTestAgg()

	// Initial deltas build the book from empty.
	agg.OnDepth(DepthEvent{Symbol: "BTCUSDT", Sequence: 1, Bids: []Level{{dec("70000"), dec("0.5")}, {dec("69990"), dec("1")}}, Asks: []Level{{dec("70010"), dec("0.3")}}})
	d, ok := agg.DepthFrame("BTCUSDT", 20)
	if !ok {
		t.Fatal("no depth frame")
	}
	if d.LastUpdateID != 1 {
		t.Fatalf("lastUpdateId = %d", d.LastUpdateID)
	}
	// Bids descending.
	if len(d.Bids) != 2 || d.Bids[0][0] != "70000" || d.Bids[1][0] != "69990" {
		t.Fatalf("bids = %+v", d.Bids)
	}
	if len(d.Asks) != 1 || d.Asks[0][0] != "70010" || d.Asks[0][1] != "0.3" {
		t.Fatalf("asks = %+v", d.Asks)
	}

	// Delta removes a bid level (qty 0) and adds an ask.
	agg.OnDepth(DepthEvent{Symbol: "BTCUSDT", Sequence: 2, Bids: []Level{{dec("70000"), dec("0")}}, Asks: []Level{{dec("70005"), dec("0.2")}}})
	d, _ = agg.DepthFrame("BTCUSDT", 20)
	if len(d.Bids) != 1 || d.Bids[0][0] != "69990" {
		t.Fatalf("after delta bids = %+v", d.Bids)
	}
	// Asks ascending: 70005 then 70010.
	if len(d.Asks) != 2 || d.Asks[0][0] != "70005" || d.Asks[1][0] != "70010" {
		t.Fatalf("after delta asks = %+v", d.Asks)
	}

	// Stale delta (<= lastDepth) is ignored.
	agg.OnDepth(DepthEvent{Symbol: "BTCUSDT", Sequence: 1, Asks: []Level{{dec("99999"), dec("1")}}})
	d, _ = agg.DepthFrame("BTCUSDT", 20)
	if len(d.Asks) != 2 {
		t.Fatalf("stale delta applied: asks = %+v", d.Asks)
	}

	if bc.count("depth:BTCUSDT") != 2 {
		t.Fatalf("depth broadcasts = %d, want 2 (stale dropped)", bc.count("depth:BTCUSDT"))
	}
}

// precAgg builds a test aggregator with BTCUSDT precision loaded
// (pricePrecision=2, quantityPrecision=6) to exercise fixed-precision rendering.
func precAgg() (*Aggregator, *recBroadcaster) {
	agg, bc := newTestAgg()
	agg.SetPrecisions([]SymbolInfo{{Symbol: "BTCUSDT", PricePrecision: 2, QuantityPrecision: 6}})
	return agg, bc
}

// TestTickerPrecision checks the ticker pads prices/quoteVolume to price
// precision (2) and base volume to quantity precision (6).
func TestTickerPrecision(t *testing.T) {
	agg, bc := precAgg()
	base := int64(1762070400)
	agg.OnTrade(trade("1", "70000", "0.01", "buy", base))

	tk, _ := agg.TickerFrame("BTCUSDT")
	if tk.LastPrice != "70000.00" {
		t.Fatalf("lastPrice = %q, want 70000.00", tk.LastPrice)
	}
	if tk.High != "70000.00" || tk.Low != "70000.00" {
		t.Fatalf("high/low = %q/%q, want 70000.00", tk.High, tk.Low)
	}
	if tk.Volume != "0.010000" {
		t.Fatalf("volume = %q, want 0.010000", tk.Volume)
	}
	// quoteVolume = 70000 * 0.01 = 700, padded to quote (price) precision 2.
	if tk.QuoteVolume != "700.00" {
		t.Fatalf("quoteVolume = %q, want 700.00", tk.QuoteVolume)
	}

	// The WS ticker push carries the same padded frame.
	var pushed tickerDTO
	for _, f := range bc.frames {
		if f.channel == "ticker:BTCUSDT" {
			pushed = f.data.(tickerDTO)
		}
	}
	if pushed.LastPrice != "70000.00" || pushed.Volume != "0.010000" || pushed.QuoteVolume != "700.00" {
		t.Fatalf("ws ticker push = %+v, want padded", pushed)
	}
}

// TestDepthPrecision checks REST depth and the WS depth push pad price to price
// precision (2) and quantity to quantity precision (6).
func TestDepthPrecision(t *testing.T) {
	agg, bc := precAgg()
	agg.OnDepth(DepthEvent{Symbol: "BTCUSDT", Sequence: 1,
		Bids: []Level{{dec("70000"), dec("0.5")}}, Asks: []Level{{dec("70010"), dec("0.3")}}})

	d, _ := agg.DepthFrame("BTCUSDT", 20)
	if d.Bids[0][0] != "70000.00" || d.Bids[0][1] != "0.500000" {
		t.Fatalf("rest bid = %v, want [70000.00 0.500000]", d.Bids[0])
	}
	if d.Asks[0][0] != "70010.00" || d.Asks[0][1] != "0.300000" {
		t.Fatalf("rest ask = %v, want [70010.00 0.300000]", d.Asks[0])
	}

	// The WS depth push (the delta that was broadcast) is padded too.
	var pushed depthDTO
	for _, f := range bc.frames {
		if f.channel == "depth:BTCUSDT" {
			pushed = f.data.(depthDTO)
		}
	}
	if pushed.Bids[0][0] != "70000.00" || pushed.Bids[0][1] != "0.500000" {
		t.Fatalf("ws depth bid = %v, want [70000.00 0.500000]", pushed.Bids[0])
	}
	if pushed.Asks[0][0] != "70010.00" || pushed.Asks[0][1] != "0.300000" {
		t.Fatalf("ws depth ask = %v, want [70010.00 0.300000]", pushed.Asks[0])
	}
}

func TestDepthLimit(t *testing.T) {
	agg, _ := newTestAgg()
	agg.OnDepth(DepthEvent{Symbol: "BTCUSDT", Sequence: 1,
		Bids: []Level{{dec("70000"), dec("1")}, {dec("69990"), dec("1")}, {dec("69980"), dec("1")}}})
	d, _ := agg.DepthFrame("BTCUSDT", 2)
	if len(d.Bids) != 2 || d.Bids[0][0] != "70000" || d.Bids[1][0] != "69990" {
		t.Fatalf("limited bids = %+v", d.Bids)
	}
}

func TestTickerFallbackMidThenIndex(t *testing.T) {
	a := NewAggregator(nil, []string{"BTCUSDT"})
	idx := decimal.NewFromInt(77000)
	a.SetIndexSource(func(string) (decimal.Decimal, bool) { return idx, true })
	tk, _ := a.TickerFrame("BTCUSDT")
	if tk.PriceSource != "index" || tk.LastPrice != "77000" || tk.PriceChangePercent != "0" || tk.Volume != "0" {
		t.Fatalf("index fallback: %+v", tk)
	}
	a.OnDepth(DepthEvent{Symbol: "BTCUSDT", Snapshot: true, Sequence: 1,
		Bids: []Level{{Price: decimal.NewFromInt(77500), Quantity: decimal.NewFromInt(1)}, {Price: decimal.NewFromInt(77400), Quantity: decimal.NewFromInt(1)}},
		Asks: []Level{{Price: decimal.NewFromInt(77700), Quantity: decimal.NewFromInt(1)}}})
	tk, _ = a.TickerFrame("BTCUSDT")
	if tk.PriceSource != "mid" || tk.LastPrice != "77600" || tk.High != "77600" {
		t.Fatalf("mid fallback: %+v", tk)
	}
	a.OnTrade(TradeEvent{Symbol: "BTCUSDT", TradeID: "1", Price: decimal.NewFromInt(77650), Quantity: decimal.NewFromInt(1), TakerSide: "buy", Time: time.Now()})
	tk, _ = a.TickerFrame("BTCUSDT")
	if tk.PriceSource != "trade" || tk.LastPrice != "77650" {
		t.Fatalf("trade: %+v", tk)
	}
}

func TestIndexCandlesWhenNoTrades(t *testing.T) {
	a := NewAggregator(nil, []string{"BTCUSDT"})
	k, _ := a.Klines("BTCUSDT", "1m", 10)
	if len(k) != 0 {
		t.Fatal("expected empty without index")
	}
	px := decimal.NewFromInt(100)
	a.SetIndexSource(func(string) (decimal.Decimal, bool) { return px, true })
	base := time.Unix(1700000040, 0)
	a.SampleIndex(base)
	px = decimal.NewFromInt(105)
	a.SampleIndex(base.Add(10 * time.Second))
	px = decimal.NewFromInt(99)
	a.SampleIndex(base.Add(70 * time.Second))
	k, _ = a.Klines("BTCUSDT", "1m", 10)
	if len(k) != 2 || k[0].Source != "index" || k[0].High != "105" || k[0].Close != "105" || k[1].Open != "99" || k[0].Volume != "0" {
		t.Fatalf("index candles: %+v", k)
	}
	if f, ok := a.KlineFrame("BTCUSDT", "1m"); !ok || f.Source != "index" {
		t.Fatalf("frame: %+v", f)
	}
	a.OnTrade(TradeEvent{Symbol: "BTCUSDT", TradeID: "1", Price: decimal.NewFromInt(101), Quantity: decimal.NewFromInt(1), TakerSide: "buy", Time: base.Add(80 * time.Second)})
	k, _ = a.Klines("BTCUSDT", "1m", 10)
	if len(k) != 1 || k[0].Source != "" {
		t.Fatalf("trade candles should win: %+v", k)
	}
}
