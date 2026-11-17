package main

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// livefeed.go: state behind the public WS channels relayed through
// market-data every tick (alongside "mark:{SYMBOL}"):
//
//	futures-ticker:{SYMBOL}            same object as GET /futures/market/ticker?symbol=
//	futures-kline:{SYMBOL}:{interval}  the current (open) candle, REST kline shape + time/closed
//
// Candles follow GET /futures/market/klines: a bucket with local fills is a
// trade candle (volume/turnover), otherwise an index candle (source "index").
// The first bucket after a restart only covers what this process observed;
// clients merge high/low with their REST bar.

// tickerStatsTTL bounds how often the WS ticker re-runs the 24h/OI queries;
// live fields (last/mark/index/funding/change) are recomputed every tick.
const tickerStatsTTL = 5 * time.Second

type liveBar struct {
	open       int64 // bucket start, unix seconds
	o, h, l, c decimal.Decimal
	v, t       decimal.Decimal
}

func (b *liveBar) add(px decimal.Decimal) {
	if px.GreaterThan(b.h) {
		b.h = px
	}
	if px.LessThan(b.l) {
		b.l = px
	}
	b.c = px
}

type cachedStats struct {
	at time.Time
	st tickerStats
}

type liveFeed struct {
	mu    sync.Mutex
	trade map[string]*liveBar // sym|iv
	index map[string]*liveBar
	ext   map[string]extCandle
	stats map[string]cachedStats
}

type extCandle struct {
	open       int64
	o, h, l, c decimal.Decimal
	v, t       decimal.Decimal
	at         time.Time
}

func bucketOf(t time.Time, sec int64) int64 {
	a := klineAnchor(sec)
	return (t.Unix()-a)/sec*sec + a
}

func liveIntervals() []string {
	ivs := make([]string, 0, len(klineSec))
	for iv := range klineSec {
		ivs = append(ivs, iv)
	}
	sort.Slice(ivs, func(i, j int) bool { return klineSec[ivs[i]] < klineSec[ivs[j]] })
	return ivs
}

func roll(m map[string]*liveBar, key string, b int64, px decimal.Decimal) *liveBar {
	cur := m[key]
	if cur == nil || cur.open != b {
		cur = &liveBar{open: b, o: px, h: px, l: px, c: px, v: dZero, t: dZero}
		m[key] = cur
		return cur
	}
	cur.add(px)
	return cur
}

func (f *liveFeed) addTrades(sym string, trades []Trade, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.trade == nil {
		f.trade = map[string]*liveBar{}
	}
	for iv, sec := range klineSec {
		for _, tr := range trades {
			at := tr.Time
			if at.IsZero() {
				at = now
			}
			b := roll(f.trade, sym+"|"+iv, bucketOf(at, sec), tr.Price)
			b.v = b.v.Add(tr.Qty)
			b.t = b.t.Add(tr.Qty.Mul(tr.Price))
		}
	}
}

func (f *liveFeed) addIndex(sym string, px decimal.Decimal, now time.Time) {
	if px.Sign() <= 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index == nil {
		f.index = map[string]*liveBar{}
	}
	for iv, sec := range klineSec {
		roll(f.index, sym+"|"+iv, bucketOf(now, sec), px)
	}
}

func (f *liveFeed) setExt(sym, iv string, b klineBar) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ext == nil {
		f.ext = map[string]extCandle{}
	}
	f.ext[sym+"|"+iv] = extCandle{open: b.Open, o: b.O, h: b.H, l: b.L, c: b.C, v: b.V, t: b.T, at: time.Now()}
}

// PollExternalTape copies venue candles into the futures WS kline so the
// chart does not fall back to a gray index bar.
func (s *Server) PollExternalTape(ctx context.Context) {
	ivs := []string{"1m", "15m", "1h", "4h", "1d", "1w"}
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		for _, sym := range s.symbols {
			for _, iv := range ivs {
				bars, ok := s.market.ExternalKlines(ctx, sym, iv, 2)
				if ok && len(bars) > 0 {
					s.live.setExt(sym, iv, bars[len(bars)-1])
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// kline returns the current candle for sym/iv (trade wins over index).
func (f *liveFeed) kline(sym, iv string, now time.Time) (map[string]any, bool) {
	sec, ok := klineSec[iv]
	if !ok {
		return nil, false
	}
	b0 := bucketOf(now, sec)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := sym + "|" + iv
	if eb, ok := f.ext[key]; ok && now.Sub(eb.at) < 20*time.Second {
		return map[string]any{"symbol": sym, "interval": iv, "time": eb.open * 1000, "openTime": eb.open * 1000,
			"open": eb.o, "high": eb.h, "low": eb.l, "close": eb.c, "volume": eb.v, "turnover": money(eb.t), "closed": false}, true
	}
	src := ""
	b := f.trade[key]
	if b == nil || b.open != b0 {
		b, src = f.index[key], "index"
		if b == nil || b.open != b0 {
			return nil, false
		}
	}
	m := map[string]any{"symbol": sym, "interval": iv, "time": b.open * 1000, "openTime": b.open * 1000,
		"open": b.o, "high": b.h, "low": b.l, "close": b.c, "volume": b.v, "turnover": money(b.t), "closed": false}
	if src != "" {
		m["source"] = src
		m["volume"], m["turnover"] = dZero, dZero
	}
	return m, true
}

func (f *liveFeed) cachedStats(sym string, now time.Time, load func() tickerStats) tickerStats {
	f.mu.Lock()
	c, ok := f.stats[sym]
	f.mu.Unlock()
	if ok && now.Sub(c.at) < tickerStatsTTL {
		return c.st
	}
	st := load()
	f.mu.Lock()
	if f.stats == nil {
		f.stats = map[string]cachedStats{}
	}
	f.stats[sym] = cachedStats{at: now, st: st}
	f.mu.Unlock()
	return st
}

// liveFrames builds the futures-ticker frames for one tick. K lines are pushed
// by the external stream so this loop does not overwrite them with a stale bar.
func (s *Server) liveFrames(ctx context.Context) []map[string]any {
	now := s.now()
	frames := make([]map[string]any, 0, len(s.symbols))
	for _, sym := range s.symbols {
		ps := s.snapshot(sym)
		if ps.Mark.Sign() <= 0 {
			continue
		}
		st := s.live.cachedStats(sym, now, func() tickerStats { return s.loadTickerStats(ctx, sym) })
		tk := s.buildTicker(ctx, sym, ps, st)
		// Latest price and 24h stats come from the venue stream. Sending the
		// local book mid and local turnover here overwrote them every second
		// and made the header show a reference price with the wrong volume.
		for _, k := range []string{
			"lastPrice", "priceSource", "high", "low", "highPrice24h", "lowPrice24h",
			"volume", "quoteVolume", "volume24h", "turnover24h",
			"priceChangePercent", "priceChangePercent24h", "openPrice24h",
		} {
			delete(tk, k)
		}
		frames = append(frames, map[string]any{"channel": "futures-ticker:" + sym, "data": tk})
	}
	return frames
}
