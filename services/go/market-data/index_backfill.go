package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// BackfillIndex 用 Binance 公共现货 K 线灌指数蜡烛，让几乎没有本地成交的符号在重启后马上有价格历史。
// 蜡烛 source 为 index、volume 为 0，不进成交数据和 24h 成交量。尽力而为，失败只打日志。
func (a *Aggregator) BackfillIndex(ctx context.Context, base string) {
	if base == "" {
		return
	}
	base = strings.TrimRight(base, "/")
	// Only symbols Binance spot actually lists (TradFi stocks/metals such as
	// AAPLUSDT / XAUUSDT are not), so we don't spam 400 Invalid symbol logs.
	// If the listing can't be fetched, fall back to trying every symbol.
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	listed, lerr := fetchBinance(c, base)
	cancel()
	var skipped []string
	for _, sym := range a.TrackedSymbols() {
		if lerr == nil {
			if _, ok := listed[sym]; !ok {
				skipped = append(skipped, sym)
				continue
			}
		}
		for _, iv := range Intervals {
			url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=1000", base, sym, iv.Name)
			var rows [][]json.RawMessage
			c, cancel := context.WithTimeout(ctx, 6*time.Second)
			err := getJSON(c, url, &rows)
			cancel()
			if err != nil {
				log.Printf("index backfill %s %s: %v", sym, iv.Name, err)
				break // symbol likely not listed upstream; skip remaining intervals
			}
			candles := parseBinanceKlines(rows)
			a.mergeIndexHistory(sym, iv.Name, candles)
		}
	}
	if len(skipped) > 0 {
		log.Printf("index backfill: skipped %d symbols not listed on Binance spot: %s", len(skipped), strings.Join(skipped, ","))
	}
}

func parseBinanceKlines(rows [][]json.RawMessage) []Candle {
	out := make([]Candle, 0, len(rows))
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		var ot int64
		if json.Unmarshal(r[0], &ot) != nil {
			continue
		}
		var f [4]decimal.Decimal
		ok := true
		for i := 0; i < 4; i++ {
			var sv string
			if json.Unmarshal(r[i+1], &sv) != nil {
				ok = false
				break
			}
			d, err := decimal.NewFromString(sv)
			if err != nil {
				ok = false
				break
			}
			f[i] = d
		}
		if !ok {
			continue
		}
		out = append(out, Candle{OpenTime: ot / 1000, Open: f[0], High: f[1], Low: f[2], Close: f[3], Closed: true})
	}
	return out
}

// mergeIndexHistory prepends historical candles older than the first sampled
// index candle (the live sampler owns the current bucket).
func (a *Aggregator) mergeIndexHistory(symbol, interval string, hist []Candle) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil || len(hist) == 0 {
		return
	}
	s := ss.idxSeries[interval]
	if s == nil {
		return
	}
	cut := int64(1 << 62)
	if len(s.candles) > 0 {
		cut = s.candles[0].OpenTime
	}
	older := make([]Candle, 0, len(hist))
	for _, c := range hist {
		if c.OpenTime < cut {
			older = append(older, c)
		}
	}
	if n := len(older); n > 0 && len(s.candles) == 0 {
		older[n-1].Closed = false // current bucket stays open for the sampler
	}
	s.candles = append(older, s.candles...)
	if len(s.candles) > maxCandlesInMemory {
		s.candles = s.candles[len(s.candles)-maxCandlesInMemory:]
	}
}
