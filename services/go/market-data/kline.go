package main

import "github.com/shopspring/decimal"

// Interval is a supported K-line period.
type Interval struct {
	Name    string
	Seconds int64
	// anchor is the reference epoch the buckets are aligned to. 0 aligns to the
	// UTC epoch (which is a Thursday midnight); weeks are aligned to Monday.
	anchor int64
}

const mondayAnchor = 345600 // 1970-01-05T00:00:00Z, the first Monday after the epoch

// Intervals lists every interval the service aggregates and serves. It is a
// superset of the openapi KlineInterval enum (it additionally supports 30m).
var Intervals = []Interval{
	{Name: "1m", Seconds: 60},
	{Name: "5m", Seconds: 300},
	{Name: "15m", Seconds: 900},
	{Name: "30m", Seconds: 1800},
	{Name: "1h", Seconds: 3600},
	{Name: "4h", Seconds: 14400},
	{Name: "1d", Seconds: 86400},
	{Name: "1w", Seconds: 604800, anchor: mondayAnchor},
}

// intervalByName indexes Intervals for O(1) lookup.
var intervalByName = func() map[string]Interval {
	m := make(map[string]Interval, len(Intervals))
	for _, iv := range Intervals {
		m[iv.Name] = iv
	}
	return m
}()

// LookupInterval returns the interval with the given name.
func LookupInterval(name string) (Interval, bool) {
	iv, ok := intervalByName[name]
	return iv, ok
}

// bucketStart returns the open time (unix seconds) of the candle that contains
// the timestamp t for this interval.
func (iv Interval) bucketStart(t int64) int64 {
	d := (t - iv.anchor) % iv.Seconds
	if d < 0 {
		d += iv.Seconds
	}
	return t - d
}

// Candle is one OHLCV bar.
type Candle struct {
	OpenTime    int64
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Volume      decimal.Decimal
	QuoteVolume decimal.Decimal
	Trades      int64
	Closed      bool
}

// newCandle opens a fresh candle at openTime seeded by a first trade.
func newCandle(openTime int64, price, qty decimal.Decimal) Candle {
	return Candle{
		OpenTime:    openTime,
		Open:        price,
		High:        price,
		Low:         price,
		Close:       price,
		Volume:      qty,
		QuoteVolume: price.Mul(qty),
		Trades:      1,
	}
}

// add folds a trade into an existing (open) candle.
func (c *Candle) add(price, qty decimal.Decimal) {
	if price.GreaterThan(c.High) {
		c.High = price
	}
	if price.LessThan(c.Low) {
		c.Low = price
	}
	c.Close = price
	c.Volume = c.Volume.Add(qty)
	c.QuoteVolume = c.QuoteVolume.Add(price.Mul(qty))
	c.Trades++
}
