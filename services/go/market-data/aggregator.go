package main

import (
	"context"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// maxCandlesInMemory bounds the per-(symbol,interval) in-memory history. It
	// comfortably covers the REST limit (max 1000) plus the live candle.
	maxCandlesInMemory = 1500
	// recentTradesInMemory bounds the public recent-trades ring per symbol.
	recentTradesInMemory = 1000
	// ticker24h is the rolling window for 24h statistics.
	ticker24h = 24 * time.Hour
	// persistQueue is the buffered depth of the async kline writer.
	persistQueue = 8192
)

// Broadcaster delivers a data frame on a channel to interested subscribers. The
// hub implements it; the aggregator stays transport-agnostic.
type Broadcaster interface {
	Broadcast(channel string, data any)
}

// nopBroadcaster drops everything; used until a hub is attached and in tests
// that do not assert on pushes.
type nopBroadcaster struct{}

func (nopBroadcaster) Broadcast(string, any) {}

// symPrec is a trading pair's decimal precision for rendering market frames. It
// mirrors the order service's convention: prices and quote-denominated amounts
// (e.g. ticker quoteVolume) use pricePrec; base quantities use qtyPrec. `ok` is
// false when the symbol is absent from the shared catalogue, in which case the
// canonical decimal string is used so no precision is invented.
type symPrec struct {
	pricePrec int
	qtyPrec   int
	ok        bool
}

// fmtPrec renders d as a fixed-precision decimal string (never a float). When
// the symbol precision is unknown it falls back to the canonical string.
func fmtPrec(d decimal.Decimal, places int, ok bool) string {
	if !ok {
		return d.String()
	}
	return d.StringFixed(int32(places))
}

// book is an aggregated order book: price (canonical string) -> resting qty.
type book struct {
	bids map[string]decimal.Decimal
	asks map[string]decimal.Decimal
	seq  uint64
}

func newBook() *book {
	return &book{bids: map[string]decimal.Decimal{}, asks: map[string]decimal.Decimal{}}
}

func applyLevels(side map[string]decimal.Decimal, levels []Level) {
	for _, l := range levels {
		key := l.Price.String()
		if l.Quantity.Sign() <= 0 {
			delete(side, key)
		} else {
			side[key] = l.Quantity
		}
	}
}

// sortedLevels returns up to limit [price, qty] pairs, each padded to the
// trading pair's precision. bids descend by price, asks ascend.
func sortedLevels(side map[string]decimal.Decimal, desc bool, limit int, p symPrec) [][2]string {
	prices := make([]decimal.Decimal, 0, len(side))
	for k := range side {
		prices = append(prices, decimal.RequireFromString(k))
	}
	sort.Slice(prices, func(i, j int) bool {
		if desc {
			return prices[i].GreaterThan(prices[j])
		}
		return prices[i].LessThan(prices[j])
	})
	if limit > 0 && len(prices) > limit {
		prices = prices[:limit]
	}
	out := make([][2]string, 0, len(prices))
	for _, price := range prices {
		key := price.String()
		out = append(out, [2]string{
			fmtPrec(price, p.pricePrec, p.ok),
			fmtPrec(side[key], p.qtyPrec, p.ok),
		})
	}
	return out
}

// series is the candle history for one (symbol, interval).
type series struct {
	iv      Interval
	candles []Candle
}

// tickItem is one trade kept in the rolling 24h window.
type tickItem struct {
	ts    time.Time
	price decimal.Decimal
	qty   decimal.Decimal
}

// symbolState holds all per-symbol aggregated state.
type symbolState struct {
	symbol    string
	book      *book
	series    map[string]*series
	window    []tickItem
	lastPrice decimal.Decimal
	hasLast   bool
	// lastTs is the unix time of the last trade (or the recovered candle's
	// open time). A last trade older than ticker24h is stale for display.
	lastTs      int64
	recent      []tradeDTO
	lastTradeID int64
	lastDepth   uint64
	// idxSeries holds index-price candles (volume 0), sampled from the
	// external index feed. Served only while the symbol has no trade candles.
	idxSeries map[string]*series
}

// Aggregator consumes engine events and maintains klines, depth, ticker and
// recent trades. It serves REST snapshots, pushes live updates to a Broadcaster,
// and persists klines to Postgres for restart recovery.
type Aggregator struct {
	mu      sync.Mutex
	store   *Store
	bc      Broadcaster
	states  map[string]*symbolState
	symbols []string
	// prec maps a symbol to its rendering precision, loaded from the shared
	// `symbols` catalogue (same source as the order service). Set once at
	// startup via SetPrecisions and only read afterwards.
	prec    map[string]symPrec
	now     func() time.Time
	persist chan persistJob
	// index returns the external index price (nil = none). Used only as a
	// display fallback when there are no trades; never feeds trade data.
	index func(symbol string) (decimal.Decimal, bool)
}

type persistJob struct {
	symbol   string
	interval string
	candle   Candle
}

// NewAggregator builds an aggregator for the given symbols and loads any
// persisted candles back into memory.
func NewAggregator(store *Store, symbols []string) *Aggregator {
	a := &Aggregator{
		store:   store,
		bc:      nopBroadcaster{},
		states:  map[string]*symbolState{},
		symbols: append([]string(nil), symbols...),
		prec:    map[string]symPrec{},
		now:     time.Now,
		persist: make(chan persistJob, persistQueue),
	}
	for _, sym := range symbols {
		a.states[sym] = newSymbolState(sym)
	}
	return a
}

func newSymbolState(symbol string) *symbolState {
	ss := &symbolState{symbol: symbol, book: newBook(), series: map[string]*series{}, idxSeries: map[string]*series{}}
	for _, iv := range Intervals {
		ss.series[iv.Name] = &series{iv: iv}
		ss.idxSeries[iv.Name] = &series{iv: iv}
	}
	return ss
}

// SetBroadcaster attaches the live-push transport (the WS hub).
func (a *Aggregator) SetBroadcaster(bc Broadcaster) { a.bc = bc }

// SetPrecisions loads per-symbol rendering precision from the shared trading-pair
// catalogue (the `symbols` table owned by the order service) so REST and WS
// price/quantity strings are padded to each pair's rules, identical to the order
// service. Call once at startup before serving.
func (a *Aggregator) SetPrecisions(infos []SymbolInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := make(map[string]symPrec, len(infos))
	for _, si := range infos {
		m[si.Symbol] = symPrec{pricePrec: si.PricePrecision, qtyPrec: si.QuantityPrecision, ok: true}
	}
	a.prec = m
}

// Recover reloads persisted candles for every tracked symbol/interval so the
// in-memory history (and REST responses) survive a restart.
func (a *Aggregator) Recover(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, sym := range a.symbols {
		ss := a.states[sym]
		for _, iv := range Intervals {
			candles, err := a.store.RecentKlines(ctx, sym, iv.Name, maxCandlesInMemory)
			if err != nil {
				return err
			}
			ss.series[iv.Name].candles = candles
			if n := len(candles); n > 0 && candles[n-1].OpenTime >= ss.lastTs {
				ss.lastPrice = candles[n-1].Close
				ss.hasLast = true
				ss.lastTs = candles[n-1].OpenTime
			}
		}
	}
	return nil
}

// StartPersister runs the async kline writer until ctx is cancelled.
func (a *Aggregator) StartPersister(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-a.persist:
				if err := a.store.UpsertKline(ctx, job.symbol, job.interval, job.candle); err != nil {
					if ctx.Err() == nil {
						log.Printf("persist kline %s %s: %v", job.symbol, job.interval, err)
					}
				}
			}
		}
	}()
}

func (a *Aggregator) enqueue(symbol, interval string, c Candle) {
	select {
	case a.persist <- persistJob{symbol: symbol, interval: interval, candle: c}:
	default:
		// Queue full: drop. The candle will be re-upserted on the next trade in
		// the same bucket, so at most the final state of an idle bucket is lost.
		log.Printf("persist queue full, dropping %s %s @%d", symbol, interval, c.OpenTime)
	}
}

// state returns the per-symbol state, lazily creating it for symbols that were
// not pre-registered (so events for a newly listed pair are not dropped).
func (a *Aggregator) state(symbol string) *symbolState {
	ss := a.states[symbol]
	if ss == nil {
		ss = newSymbolState(symbol)
		a.states[symbol] = ss
		a.symbols = append(a.symbols, symbol)
	}
	return ss
}

// --- Sink implementation -----------------------------------------------------

// OnTrade folds a trade into klines, ticker and recent trades, then pushes.
func (a *Aggregator) OnTrade(ev TradeEvent) {
	a.mu.Lock()
	ss := a.state(ev.Symbol)

	// Dedup on the engine's monotonic trade id (best-effort re-delivery guard).
	if id, err := strconv.ParseInt(ev.TradeID, 10, 64); err == nil {
		if id <= ss.lastTradeID {
			a.mu.Unlock()
			return
		}
		ss.lastTradeID = id
	}

	ss.lastPrice = ev.Price
	ss.hasLast = true
	ss.lastTs = ev.Time.Unix()

	// Recent trades ring + 24h ticker window.
	side := ev.TakerSide
	if side != "buy" && side != "sell" {
		side = "buy"
	}
	td := tradeDTO{
		TradeID:  ev.TradeID,
		Price:    ev.Price.String(),
		Quantity: ev.Quantity.String(),
		Side:     side,
		Time:     ev.Time.UnixMilli(),
	}
	ss.recent = append(ss.recent, td)
	if len(ss.recent) > recentTradesInMemory {
		ss.recent = ss.recent[len(ss.recent)-recentTradesInMemory:]
	}
	ss.window = append(ss.window, tickItem{ts: ev.Time, price: ev.Price, qty: ev.Quantity})
	a.purgeWindow(ss)

	// Klines: update/roll every interval's current candle.
	type klinePush struct {
		interval string
		candle   Candle
	}
	var pushes []klinePush
	for _, iv := range Intervals {
		s := ss.series[iv.Name]
		bucket := iv.bucketStart(ev.Time.Unix())
		n := len(s.candles)
		if n > 0 && s.candles[n-1].OpenTime == bucket {
			s.candles[n-1].add(ev.Price, ev.Quantity)
			cur := s.candles[n-1]
			a.enqueue(ev.Symbol, iv.Name, cur)
			pushes = append(pushes, klinePush{iv.Name, cur})
		} else if n > 0 && s.candles[n-1].OpenTime > bucket {
			// Out-of-order trade (should not happen): ignore for this interval.
			continue
		} else {
			if n > 0 {
				// Close the previous candle before opening a new one.
				s.candles[n-1].Closed = true
				prev := s.candles[n-1]
				a.enqueue(ev.Symbol, iv.Name, prev)
				pushes = append(pushes, klinePush{iv.Name, prev})
			}
			c := newCandle(bucket, ev.Price, ev.Quantity)
			s.candles = append(s.candles, c)
			if len(s.candles) > maxCandlesInMemory {
				s.candles = s.candles[len(s.candles)-maxCandlesInMemory:]
			}
			a.enqueue(ev.Symbol, iv.Name, c)
			pushes = append(pushes, klinePush{iv.Name, c})
		}
	}

	ticker := a.tickerDTOLocked(ss)
	a.mu.Unlock()

	// Broadcast outside the lock.
	a.bc.Broadcast("trades:"+ev.Symbol, td)
	a.bc.Broadcast("ticker:"+ev.Symbol, ticker)
	for _, p := range pushes {
		a.bc.Broadcast("kline:"+ev.Symbol+":"+p.interval, toWSKline(p.candle))
	}
}

// OnDepth applies a snapshot/delta to the book and pushes the delta.
func (a *Aggregator) OnDepth(ev DepthEvent) {
	a.mu.Lock()
	ss := a.state(ev.Symbol)
	if ev.Snapshot {
		b := newBook()
		applyLevels(b.bids, ev.Bids)
		applyLevels(b.asks, ev.Asks)
		b.seq = ev.Sequence
		ss.book = b
		ss.lastDepth = ev.Sequence
	} else {
		if ev.Sequence != 0 && ev.Sequence <= ss.lastDepth {
			a.mu.Unlock()
			return // stale / duplicate delta
		}
		applyLevels(ss.book.bids, ev.Bids)
		applyLevels(ss.book.asks, ev.Asks)
		ss.book.seq = ev.Sequence
		ss.lastDepth = ev.Sequence
	}
	p := a.prec[ev.Symbol]
	a.mu.Unlock()

	a.bc.Broadcast("depth:"+ev.Symbol, depthDTO{
		Symbol:       ev.Symbol,
		LastUpdateID: ev.Sequence,
		Bids:         levelsToPairs(ev.Bids, p),
		Asks:         levelsToPairs(ev.Asks, p),
	})
}

func levelsToPairs(levels []Level, p symPrec) [][2]string {
	out := make([][2]string, 0, len(levels))
	for _, l := range levels {
		out = append(out, [2]string{
			fmtPrec(l.Price, p.pricePrec, p.ok),
			fmtPrec(l.Quantity, p.qtyPrec, p.ok),
		})
	}
	return out
}

func (a *Aggregator) purgeWindow(ss *symbolState) {
	cutoff := a.now().Add(-ticker24h)
	i := 0
	for i < len(ss.window) && ss.window[i].ts.Before(cutoff) {
		i++
	}
	if i > 0 {
		ss.window = ss.window[i:]
	}
}

// --- Snapshots for REST + WS -------------------------------------------------

// HasSymbol reports whether the symbol is tracked.
func (a *Aggregator) HasSymbol(symbol string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.states[symbol]
	return ok
}

// TrackedSymbols returns the tracked symbol names.
func (a *Aggregator) TrackedSymbols() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.symbols...)
}

// DepthFrame returns a depth snapshot DTO for the symbol.
func (a *Aggregator) DepthFrame(symbol string, limit int) (depthDTO, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil {
		return depthDTO{}, false
	}
	p := a.prec[symbol]
	return depthDTO{
		Symbol:       symbol,
		LastUpdateID: ss.book.seq,
		Bids:         sortedLevels(ss.book.bids, true, limit, p),
		Asks:         sortedLevels(ss.book.asks, false, limit, p),
	}, true
}

// TickerFrame returns a 24h ticker DTO for the symbol.
func (a *Aggregator) TickerFrame(symbol string) (tickerDTO, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil {
		return tickerDTO{}, false
	}
	a.purgeWindow(ss)
	return a.tickerDTOLocked(ss), true
}

// tickerDTOLocked computes the ticker from the rolling window. Caller holds mu.
// Prices (lastPrice/high/low) render to pricePrecision, base volume to
// quantityPrecision, and quoteVolume to pricePrecision (the quote currency's
// precision), matching the order service's fixed-precision decimal strings.
func (a *Aggregator) tickerDTOLocked(ss *symbolState) tickerDTO {
	p := a.prec[ss.symbol]
	last, src := a.displayPriceLocked(ss)
	dto := tickerDTO{
		Symbol:             ss.symbol,
		PriceSource:        src,
		LastPrice:          fmtPrec(last, p.pricePrec, p.ok),
		PriceChangePercent: "0",
		High:               fmtPrec(last, p.pricePrec, p.ok),
		Low:                fmtPrec(last, p.pricePrec, p.ok),
		Volume:             fmtPrec(decimal.Zero, p.qtyPrec, p.ok),
		QuoteVolume:        fmtPrec(decimal.Zero, p.pricePrec, p.ok),
	}
	if len(ss.window) == 0 {
		// No local trades in 24h: derive change/high/low from the external
		// index candles (volume stays 0 — no fabricated volume).
		if src == "index" || src == "mid" {
			if is := ss.idxSeries["1m"]; is != nil {
				from := a.now().Add(-ticker24h).Unix()
				var open, hi, lo decimal.Decimal
				found := false
				for _, c := range is.candles {
					if c.OpenTime < from {
						continue
					}
					if !found {
						open, hi, lo, found = c.Open, c.High, c.Low, true
					}
					if c.High.GreaterThan(hi) {
						hi = c.High
					}
					if c.Low.LessThan(lo) {
						lo = c.Low
					}
				}
				if found {
					if last.GreaterThan(hi) {
						hi = last
					}
					if last.LessThan(lo) {
						lo = last
					}
					dto.High = fmtPrec(hi, p.pricePrec, p.ok)
					dto.Low = fmtPrec(lo, p.pricePrec, p.ok)
					if open.Sign() > 0 {
						dto.PriceChangePercent = last.Sub(open).Div(open).Mul(decimal.NewFromInt(100)).Round(2).String()
					}
				}
			}
		}
		return dto
	}
	high := ss.window[0].price
	low := ss.window[0].price
	vol := decimal.Zero
	quoteVol := decimal.Zero
	open := ss.window[0].price
	for _, it := range ss.window {
		if it.price.GreaterThan(high) {
			high = it.price
		}
		if it.price.LessThan(low) {
			low = it.price
		}
		vol = vol.Add(it.qty)
		quoteVol = quoteVol.Add(it.price.Mul(it.qty))
	}
	dto.High = fmtPrec(high, p.pricePrec, p.ok)
	dto.Low = fmtPrec(low, p.pricePrec, p.ok)
	dto.Volume = fmtPrec(vol, p.qtyPrec, p.ok)
	dto.QuoteVolume = fmtPrec(quoteVol, p.pricePrec, p.ok)
	if open.Sign() > 0 {
		pct := last.Sub(open).Div(open).Mul(decimal.NewFromInt(100))
		dto.PriceChangePercent = pct.Round(2).String()
	}
	return dto
}

// Klines returns up to limit candles (ascending) for a symbol/interval.
func (a *Aggregator) Klines(symbol, interval string, limit int) ([]restKline, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil {
		return nil, false
	}
	s := ss.series[interval]
	if s == nil {
		return nil, false
	}
	out := mergeKlines(s.candles, ss.idxSeries[interval])
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, true
}

// mergeKlines unions trade candles with index-price candles by open time.
// Trade candles win; index candles (source "index", volume 0) fill buckets
// with no local trades so a thin market still renders a continuous chart.
func mergeKlines(trade []Candle, idx *series) []restKline {
	var ic []Candle
	if idx != nil {
		ic = idx.candles
	}
	out := make([]restKline, 0, len(trade)+len(ic))
	i, j := 0, 0
	for i < len(trade) || j < len(ic) {
		switch {
		case j >= len(ic) || (i < len(trade) && trade[i].OpenTime < ic[j].OpenTime):
			out = append(out, toRESTKline(trade[i]))
			i++
		case i >= len(trade) || ic[j].OpenTime < trade[i].OpenTime:
			k := toRESTKline(ic[j])
			k.Source = "index"
			out = append(out, k)
			j++
		default: // same bucket: trade wins
			out = append(out, toRESTKline(trade[i]))
			i++
			j++
		}
	}
	return out
}

// KlineFrame returns the current (live) candle for a symbol/interval, for the
// initial WS push on subscribe.
func (a *Aggregator) KlineFrame(symbol, interval string) (wsKline, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil {
		return wsKline{}, false
	}
	s := ss.series[interval]
	if s == nil {
		return wsKline{}, false
	}
	is := ss.idxSeries[interval]
	hasIdx := is != nil && len(is.candles) > 0
	if len(s.candles) == 0 || (hasIdx && is.candles[len(is.candles)-1].OpenTime > s.candles[len(s.candles)-1].OpenTime) {
		if !hasIdx {
			return wsKline{}, false
		}
		k := toWSKline(is.candles[len(is.candles)-1])
		k.Source = "index"
		return k, true
	}
	return toWSKline(s.candles[len(s.candles)-1]), true
}

// RecentTrades returns up to limit recent public trades, newest first.
func (a *Aggregator) RecentTrades(symbol string, limit int) ([]tradeDTO, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil {
		return nil, false
	}
	n := len(ss.recent)
	if limit > 0 && n > limit {
		n = limit
	}
	out := make([]tradeDTO, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ss.recent[len(ss.recent)-1-i])
	}
	return out, true
}

// ValidInterval reports whether the interval name is supported.
func (a *Aggregator) ValidInterval(name string) bool {
	_, ok := LookupInterval(name)
	return ok
}

// LastPrice returns the last local spot trade price for symbol.
func (a *Aggregator) LastPrice(symbol string) (decimal.Decimal, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.states[symbol]
	if ss == nil || !ss.hasLast {
		return decimal.Zero, false
	}
	return ss.lastPrice, true
}

// SetIndexSource wires the external index price used as a display fallback.
func (a *Aggregator) SetIndexSource(f func(symbol string) (decimal.Decimal, bool)) {
	a.mu.Lock()
	a.index = f
	a.mu.Unlock()
}

// bestLocked returns the best price on one side of the book.
func bestLocked(side map[string]decimal.Decimal, bid bool) (decimal.Decimal, bool) {
	var best decimal.Decimal
	found := false
	for k, q := range side {
		if !q.IsPositive() {
			continue
		}
		px, err := decimal.NewFromString(k)
		if err != nil || !px.IsPositive() {
			continue
		}
		if !found || (bid && px.GreaterThan(best)) || (!bid && px.LessThan(best)) {
			best, found = px, true
		}
	}
	return best, found
}

// displayPriceLocked picks the ticker's lastPrice: last trade, else mid of the
// best bid/ask, else the external index. Caller holds mu.
func (a *Aggregator) displayPriceLocked(ss *symbolState) (decimal.Decimal, string) {
	fresh := ss.hasLast && a.now().Unix()-ss.lastTs < int64(ticker24h/time.Second)
	if fresh {
		return ss.lastPrice, "trade"
	}
	bid, okB := bestLocked(ss.book.bids, true)
	ask, okA := bestLocked(ss.book.asks, false)
	if okB && okA && ask.GreaterThanOrEqual(bid) {
		return bid.Add(ask).Div(decimal.NewFromInt(2)), "mid"
	}
	if a.index != nil {
		if px, ok := a.index(ss.symbol); ok && px.IsPositive() {
			return px, "index"
		}
	}
	if ss.hasLast {
		return ss.lastPrice, "trade"
	}
	return decimal.Zero, "none"
}

// SampleIndex folds the current index price of every tracked symbol into its
// index-candle series (volume 0). Call periodically (see StartIndexSampler).
func (a *Aggregator) SampleIndex(now time.Time) {
	type push struct {
		ch   string
		data any
	}
	var pushes []push
	a.mu.Lock()
	defer func() {
		a.mu.Unlock()
		for _, p := range pushes {
			a.bc.Broadcast(p.ch, p.data)
		}
	}()
	if a.index == nil {
		return
	}
	ts := now.Unix()
	for _, ss := range a.states {
		px, ok := a.index(ss.symbol)
		if !ok || !px.IsPositive() {
			continue
		}
		noTrades := len(ss.window) == 0
		for _, iv := range Intervals {
			s := ss.idxSeries[iv.Name]
			bucket := iv.bucketStart(ts)
			n := len(s.candles)
			switch {
			case n > 0 && s.candles[n-1].OpenTime == bucket:
				s.candles[n-1].add(px, decimal.Zero)
				s.candles[n-1].Trades = 0
			case n > 0 && s.candles[n-1].OpenTime > bucket:
				continue
			default:
				if n > 0 {
					s.candles[n-1].Closed = true
				}
				c := newCandle(bucket, px, decimal.Zero)
				c.Trades = 0
				s.candles = append(s.candles, c)
				if len(s.candles) > maxCandlesInMemory {
					s.candles = s.candles[len(s.candles)-maxCandlesInMemory:]
				}
			}
		}
		if noTrades {
			for _, iv := range Intervals {
				if s := ss.idxSeries[iv.Name]; len(s.candles) > 0 && len(ss.series[iv.Name].candles) == 0 ||
					len(s.candles) > 0 && s.candles[len(s.candles)-1].OpenTime > ss.series[iv.Name].candles[len(ss.series[iv.Name].candles)-1].OpenTime {
					k := toWSKline(s.candles[len(s.candles)-1])
					k.Source = "index"
					pushes = append(pushes, push{"kline:" + ss.symbol + ":" + iv.Name, k})
				}
			}
			pushes = append(pushes, push{"ticker:" + ss.symbol, a.tickerDTOLocked(ss)})
		}
	}
}

// StartIndexSampler samples the index every period until ctx is done.
func (a *Aggregator) StartIndexSampler(ctx context.Context, period time.Duration) {
	if period <= 0 {
		period = 2 * time.Second
	}
	go func() {
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				a.SampleIndex(now)
			}
		}
	}()
}
