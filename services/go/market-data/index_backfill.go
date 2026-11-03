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

// BackfillIndex seeds the index-candle series from Binance public spot klines
// so a symbol with few or no local trades shows real price history right after
// a restart (instead of a couple of candles sampled since boot). Candles are
// marked source "index" with volume 0; they never feed trade data or the 24h
// volume. Best-effort: failures are logged and ignored.
func (a *Aggregator) BackfillIndex(ctx context.Context, base string) {
	if base == "" {
		return
	}
	base = strings.TrimRight(base, "/")
	for _, sym := range a.TrackedSymbols() {
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
