package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestSortTickers(t *testing.T) {
	base := []tickerDTO{
		{Symbol: "AAA", PriceChangePercent: "1.5", QuoteVolume: "10", PriceSource: "trade"},
		{Symbol: "BBB", PriceChangePercent: "-3", QuoteVolume: "500", PriceSource: "trade"},
		{Symbol: "CCC", PriceChangePercent: "7.25", QuoteVolume: "50", PriceSource: "index"},
		{Symbol: "ZZZ", PriceChangePercent: "99", QuoteVolume: "9999", PriceSource: "none"},
	}
	order := func(by string) string {
		ts := append([]tickerDTO(nil), base...)
		sortTickers(ts, by)
		s := ""
		for _, x := range ts {
			s += x.Symbol + ","
		}
		return s
	}
	if got := order("gainers"); got != "CCC,AAA,BBB,ZZZ," {
		t.Fatalf("gainers %s", got)
	}
	if got := order("losers"); got != "BBB,AAA,CCC,ZZZ," {
		t.Fatalf("losers %s", got)
	}
	if got := order("volume"); got != "BBB,CCC,AAA,ZZZ," {
		t.Fatalf("volume %s", got)
	}
	if order("gainers") == order("losers") {
		t.Fatal("gainers must differ from losers")
	}
}

func TestIndexTicker24hChange(t *testing.T) {
	a := NewAggregator(nil, []string{"BTCUSDT"})
	now := time.Unix(1_800_000_000, 0)
	a.now = func() time.Time { return now }
	px := decimal.NewFromInt(110)
	a.SetIndexSource(func(string) (decimal.Decimal, bool) { return px, true })
	// Backfilled 5m index history: 100 at 25h ago .. 110 now.
	var hist []Candle
	for ts := now.Add(-25 * time.Hour).Unix(); ts < now.Unix(); ts += 300 {
		hist = append(hist, Candle{OpenTime: ts - ts%300, Open: decimal.NewFromInt(100), High: decimal.NewFromInt(120), Low: decimal.NewFromInt(90), Close: decimal.NewFromInt(100), Closed: true})
	}
	a.mergeIndexHistory("BTCUSDT", "5m", hist)
	tk, _ := a.TickerFrame("BTCUSDT")
	if tk.PriceSource != "index" || tk.PriceChangePercent != "10" || tk.High != "120" || tk.Low != "90" || tk.Volume != "0" {
		t.Fatalf("index ticker: %+v", tk)
	}
}

func TestRecoverTradesRestoresWindow(t *testing.T) {
	a := NewAggregator(nil, []string{"ETHUSDT"})
	now := time.Now()
	a.RecoverTrades([]TradeEvent{
		{TradeID: "5", Symbol: "ETHUSDT", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(2), TakerSide: "buy", Time: now.Add(-2 * time.Hour)},
		{TradeID: "6", Symbol: "ETHUSDT", Price: decimal.NewFromInt(105), Quantity: decimal.NewFromInt(1), TakerSide: "sell", Time: now.Add(-time.Hour)},
		{TradeID: "7", Symbol: "NOPE", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), Time: now},
	})
	tk, _ := a.TickerFrame("ETHUSDT")
	if tk.PriceSource != "trade" || tk.LastPrice != "105" || tk.PriceChangePercent != "5" || tk.Volume != "3" || tk.QuoteVolume != "305" {
		t.Fatalf("recovered ticker: %+v", tk)
	}
	tr, _ := a.RecentTrades("ETHUSDT", 10)
	if len(tr) != 2 || tr[0].TradeID != "6" || tr[0].Side != "sell" {
		t.Fatalf("recent: %+v", tr)
	}
	// A re-delivered old trade is deduped.
	a.OnTrade(TradeEvent{TradeID: "6", Symbol: "ETHUSDT", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), Time: now})
	if tk2, _ := a.TickerFrame("ETHUSDT"); tk2.LastPrice != "105" {
		t.Fatalf("dedup failed: %+v", tk2)
	}
}

func TestNormInterval(t *testing.T) {
	for in, want := range map[string]string{"1W": "1w", "1D": "1d", "4H": "4h", "1H": "1h", "1m": "1m", "1M": "1M"} {
		if got := normInterval(in); got != want {
			t.Errorf("normInterval(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDropEmptyCategories(t *testing.T) {
	out := dropEmptyCategories(filterCategories([]Category{{ID: "stocks", Symbols: []string{"AAPLUSDT"}}, {ID: "l1", Symbols: []string{"BTCUSDT"}}}, map[string]bool{"BTCUSDT": true}))
	if len(out) != 1 || out[0].ID != "l1" {
		t.Fatalf("got %+v", out)
	}
}
