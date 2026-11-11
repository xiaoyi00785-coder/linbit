package main

// 公开 WS 的内存状态，每个 tick 经行情服务转发，和 "mark:{SYMBOL}" 一起推：
// futures-ticker:{SYMBOL} 与 GET /futures/market/ticker?symbol= 同结构；
// futures-kline:{SYMBOL}:{interval} 是当前未收盘 K 线，REST K 线形状加 time/closed。
// 有本地成交的桶是成交 K（量/额），否则是指数 K（source "index"）。
// 进程重启后的第一根只覆盖本进程看到的成交；客户端用 REST 柱合并高低价。

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// tickerStatsTTL：WS ticker 最多每 5 秒重跑 24h/持仓量查询。最新价、标记价、指数、资金费、涨跌每个 tick 都重算。
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
	stats map[string]cachedStats
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

// liveFrames builds the futures-ticker and futures-kline frames for one tick.
func (s *Server) liveFrames(ctx context.Context) []map[string]any {
	now := s.now()
	ivs := liveIntervals()
	frames := make([]map[string]any, 0, len(s.symbols)*(1+len(ivs)))
	for _, sym := range s.symbols {
		ps := s.snapshot(sym)
		if ps.Mark.Sign() <= 0 {
			continue
		}
		st := s.live.cachedStats(sym, now, func() tickerStats { return s.loadTickerStats(ctx, sym) })
		frames = append(frames, map[string]any{"channel": "futures-ticker:" + sym, "data": s.buildTicker(ctx, sym, ps, st)})
		for _, iv := range ivs {
			if k, ok := s.live.kline(sym, iv, now); ok {
				frames = append(frames, map[string]any{"channel": "futures-kline:" + sym + ":" + iv, "data": k})
			}
		}
	}
	return frames
}
