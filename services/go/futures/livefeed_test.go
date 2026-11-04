package main

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestDisplayPriceStaleTradeFallsBack(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://x:x@127.0.0.1:1/x?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Unix(1_800_000_000, 0)
	s := &Server{db: db, now: func() time.Time { return now }, cfg: Config{LastTradeFresh: 5 * time.Second}}
	ps := priceState{Index: d("60000"), Mark: d("60010"), Last: d("59000")}
	ctx := context.Background()
	for _, tc := range []struct {
		at  time.Time
		px  string
		src string
	}{
		{now.Add(-2 * time.Second), "59000", "trade"},
		{now.Add(-15 * time.Second), "60000", "index"}, // stale fill: index, not the frozen trade
		{time.Time{}, "60000", "index"},                // loaded at startup: never "fresh"
	} {
		ps.LastAt = tc.at
		px, src := s.displayPrice(ctx, "BTCUSDT", ps)
		if src != tc.src || !px.Equal(d(tc.px)) {
			t.Fatalf("at=%v: got %s/%s want %s/%s", tc.at, px, src, tc.px, tc.src)
		}
	}
}

func TestLiveKlineTradeWinsAndRolls(t *testing.T) {
	var f liveFeed
	t0 := time.Unix(1_800_000_000, 0) // 60s-aligned
	if _, ok := f.kline("BTCUSDT", "1m", t0); ok {
		t.Fatal("no bar yet")
	}
	f.addIndex("BTCUSDT", d("100"), t0)
	f.addIndex("BTCUSDT", d("103"), t0.Add(time.Second))
	f.addIndex("BTCUSDT", d("99"), t0.Add(2*time.Second))
	k, ok := f.kline("BTCUSDT", "1m", t0.Add(3*time.Second))
	if !ok || k["source"] != "index" || k["time"].(int64) != t0.Unix()*1000 {
		t.Fatalf("index bar %v", k)
	}
	if !eqD(k["open"], "100") || !eqD(k["high"], "103") || !eqD(k["low"], "99") || !eqD(k["close"], "99") || !eqD(k["volume"], "0") {
		t.Fatalf("index ohlc %v", k)
	}
	f.addTrades("BTCUSDT", []Trade{{Price: d("101"), Qty: d("2"), Time: t0.Add(4 * time.Second)}, {Price: d("102"), Qty: d("1"), Time: t0.Add(5 * time.Second)}}, t0)
	k, _ = f.kline("BTCUSDT", "1m", t0.Add(6*time.Second))
	if _, idx := k["source"]; idx || !eqD(k["open"], "101") || !eqD(k["close"], "102") || !eqD(k["volume"], "3") || !eqD(k["turnover"], "304") {
		t.Fatalf("trade bar %v", k)
	}
	// next minute: trade bar is old, index bar rolls fresh
	f.addIndex("BTCUSDT", d("105"), t0.Add(61*time.Second))
	k, _ = f.kline("BTCUSDT", "1m", t0.Add(62*time.Second))
	if k["source"] != "index" || k["time"].(int64) != (t0.Unix()+60)*1000 || !eqD(k["open"], "105") {
		t.Fatalf("rolled bar %v", k)
	}
	// the 1h bar still carries the trades
	if k, _ := f.kline("BTCUSDT", "1h", t0.Add(62*time.Second)); !eqD(k["volume"], "3") {
		t.Fatalf("1h bar %v", k)
	}
	if _, ok := f.kline("BTCUSDT", "7m", t0); ok {
		t.Fatal("bad interval")
	}
}

func eqD(v any, want string) bool {
	x, ok := v.(interface{ String() string })
	return ok && d(x.String()).Equal(d(want))
}
