package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// extfeed.go: public candles and prints copied from Binance (spot, then USDⓈ-M)
// with OKX as backup. These replace the gray index candles and the local
// market-maker tape on the chart. Local fills still update the ticker.

const (
	extTipTTL  = 3 * time.Second
	extHistTTL = 45 * time.Second
	extLiveFor = 30 * time.Second
)

var extChartIntervals = []string{"1m", "15m", "1h", "4h", "1d", "1w"}

// Instruments that are not the same id on Binance USDⓈ-M.
var extFAPIAlias = map[string]string{
	"PAXGUSDT": "XAUUSDT",
	"XAUTUSDT": "XAUUSDT",
	"WTIUSDT":  "CLUSDT",
}

type extSnap struct {
	klines []restKline
	trades []tradeDTO
	at     time.Time
}

type extFeed struct {
	binance string
	fapi    string
	okx     string
	client  *http.Client
	now     func() time.Time

	mu       sync.Mutex
	cache    map[string]extSnap
	inflight map[string]chan struct{}
	lastID   map[string]string
	liveAt   map[string]time.Time
	books    map[string]depthDTO
	seq      map[string]uint64
	ticks    map[string]tickerDTO

	// Futures books are kept apart from spot. Both used to share `books`, so
	// /market/depth flipped between the Binance spot book and the OKX swap
	// book depending on which venue wrote last.
	futBooks map[string]depthDTO
	okxBooks map[string]*okxBook        // instId -> full local book (channel "books")
	okxCt    map[string]decimal.Decimal // instId -> contract value in base coin
	okxEmit  map[string]time.Time       // instId -> last futures-depth broadcast
}

func newExtFeed(binance, fapi, okx string) *extFeed {
	return &extFeed{
		binance:  strings.TrimRight(binance, "/"),
		fapi:     strings.TrimRight(fapi, "/"),
		okx:      strings.TrimRight(okx, "/"),
		client:   &http.Client{Timeout: 8 * time.Second},
		now:      time.Now,
		cache:    map[string]extSnap{},
		inflight: map[string]chan struct{}{},
		lastID:   map[string]string{},
		liveAt:   map[string]time.Time{},
		books:    map[string]depthDTO{},
		seq:      map[string]uint64{},
		ticks:    map[string]tickerDTO{},
		futBooks: map[string]depthDTO{},
		okxBooks: map[string]*okxBook{},
		okxCt:    map[string]decimal.Decimal{},
		okxEmit:  map[string]time.Time{},
	}
}

func (e *extFeed) hasDepth(symbol string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	symbol = strings.ToUpper(symbol)
	if d, ok := e.books[symbol]; ok && len(d.Bids)+len(d.Asks) > 0 {
		return true
	}
	d, ok := e.futBooks[symbol]
	return ok && len(d.Bids)+len(d.Asks) > 0
}

// depth returns the spot venue book. Symbols that only trade as perps
// (stocks, metals, oil) have no spot book and fall back to the swap book.
func (e *extFeed) depth(symbol string, limit int) (depthDTO, bool) {
	return e.venueDepth(symbol, limit, false)
}

// futDepth returns the swap venue book, falling back to spot when the swap
// stream has not delivered yet.
func (e *extFeed) futDepth(symbol string, limit int) (depthDTO, bool) {
	return e.venueDepth(symbol, limit, true)
}

func (e *extFeed) venueDepth(symbol string, limit int, futures bool) (depthDTO, bool) {
	if e == nil {
		return depthDTO{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	symbol = strings.ToUpper(symbol)
	first, second := e.books, e.futBooks
	if futures {
		first, second = e.futBooks, e.books
	}
	d, ok := first[symbol]
	if !ok || len(d.Bids)+len(d.Asks) == 0 {
		d, ok = second[symbol]
	}
	if !ok || len(d.Bids)+len(d.Asks) == 0 {
		return depthDTO{}, false
	}
	if limit > 0 && len(d.Bids) > limit {
		d.Bids = append([][2]string(nil), d.Bids[:limit]...)
	}
	if limit > 0 && len(d.Asks) > limit {
		d.Asks = append([][2]string(nil), d.Asks[:limit]...)
	}
	d.Snapshot = true
	return d, true
}

func (e *extFeed) live(symbol string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	at, ok := e.liveAt[strings.ToUpper(symbol)]
	return ok && e.now().Sub(at) < extLiveFor
}

func (e *extFeed) markLive(symbol string) {
	e.mu.Lock()
	e.liveAt[strings.ToUpper(symbol)] = e.now()
	e.mu.Unlock()
}

func extCacheKey(kind, symbol, interval string, hist bool) string {
	h := "tip"
	if hist {
		h = "hist"
	}
	return kind + "|" + strings.ToUpper(symbol) + "|" + interval + "|" + h
}

func (e *extFeed) cached(key string, ttl time.Duration, fetch func() (extSnap, error)) (extSnap, error) {
	now := e.now()
	e.mu.Lock()
	if s, ok := e.cache[key]; ok && now.Sub(s.at) < ttl && (len(s.klines) > 0 || len(s.trades) > 0) {
		e.mu.Unlock()
		return s, nil
	}
	if ch, ok := e.inflight[key]; ok {
		e.mu.Unlock()
		<-ch
		e.mu.Lock()
		s := e.cache[key]
		e.mu.Unlock()
		if len(s.klines) == 0 && len(s.trades) == 0 {
			return extSnap{}, fmt.Errorf("empty")
		}
		return s, nil
	}
	ch := make(chan struct{})
	e.inflight[key] = ch
	e.mu.Unlock()

	snap, err := fetch()
	e.mu.Lock()
	if err == nil && (len(snap.klines) > 0 || len(snap.trades) > 0) {
		snap.at = e.now()
		e.cache[key] = snap
	}
	delete(e.inflight, key)
	close(ch)
	e.mu.Unlock()
	return snap, err
}

// Klines returns external candles, oldest first. ok is false when every venue fails.
func (e *extFeed) Klines(ctx context.Context, symbol, interval string, limit int) ([]restKline, bool) {
	if e == nil || limit <= 0 {
		return nil, false
	}
	hist := limit > 5
	ttl := extTipTTL
	if hist {
		ttl = extHistTTL
	}
	snap, err := e.cached(extCacheKey("k", symbol, interval, hist), ttl, func() (extSnap, error) {
		rows, err := e.fetchKlines(ctx, symbol, interval, limit)
		if err != nil {
			return extSnap{}, err
		}
		return extSnap{klines: rows}, nil
	})
	if err != nil || len(snap.klines) == 0 {
		return nil, false
	}
	e.markLive(symbol)
	out := snap.klines
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, true
}

// Trades returns external prints, newest first.
func (e *extFeed) Trades(ctx context.Context, symbol string, limit int) ([]tradeDTO, bool) {
	if e == nil {
		return nil, false
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	snap, err := e.cached(extCacheKey("t", symbol, "", false), extTipTTL, func() (extSnap, error) {
		rows, err := e.fetchTrades(ctx, symbol, limit)
		if err != nil {
			return extSnap{}, err
		}
		return extSnap{trades: rows}, nil
	})
	if err != nil || len(snap.trades) == 0 {
		return nil, false
	}
	e.markLive(symbol)
	out := snap.trades
	if len(out) > limit {
		out = out[:limit]
	}
	return out, true
}

// Frame is the latest external candle for the WS snapshot.
func (e *extFeed) Frame(symbol, interval string) (wsKline, bool) {
	rows, ok := e.Klines(context.Background(), symbol, interval, 2)
	if !ok || len(rows) == 0 {
		return wsKline{}, false
	}
	k := rows[len(rows)-1]
	return wsKline{Time: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume, Closed: false}, true
}

func (e *extFeed) run(ctx context.Context, symbols []string, bc Broadcaster) {
	if e == nil || bc == nil {
		return
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		e.refresh(ctx, symbols, nil, true)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (e *extFeed) refresh(ctx context.Context, symbols []string, bc Broadcaster, full bool) {
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, sym := range symbols {
		sym := sym
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			e.refreshOne(ctx, sym, bc, full)
		}()
	}
	wg.Wait()
}

func (e *extFeed) refreshOne(ctx context.Context, symbol string, bc Broadcaster, full bool) {
	ivs := extChartIntervals
	if !full {
		ivs = []string{"1m", "15m", "1h", "4h", "1d", "1w"}
	}
	for _, iv := range ivs {
		limit := 2
		if full {
			limit = 500
		}
		rows, ok := e.Klines(ctx, symbol, iv, limit)
		if !ok || len(rows) == 0 || bc == nil {
			continue
		}
		k := rows[len(rows)-1]
		bc.Broadcast("kline:"+symbol+":"+iv, wsKline{Time: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	trades, ok := e.Trades(ctx, symbol, 20)
	if !ok || len(trades) == 0 || bc == nil {
		return
	}
	e.mu.Lock()
	prev := e.lastID[strings.ToUpper(symbol)]
	e.lastID[strings.ToUpper(symbol)] = trades[0].TradeID
	e.mu.Unlock()
	fresh := trades
	if prev != "" {
		fresh = nil
		for _, tr := range trades {
			if tr.TradeID == prev {
				break
			}
			fresh = append(fresh, tr)
		}
	}
	for i := len(fresh) - 1; i >= 0; i-- {
		bc.Broadcast("trades:"+symbol, fresh[i])
	}
}

func (e *extFeed) fetchKlines(ctx context.Context, symbol, interval string, limit int) ([]restKline, error) {
	if limit > 1000 {
		limit = 1000
	}
	if limit < 2 {
		limit = 2
	}
	sym := strings.ToUpper(symbol)
	var last error
	for _, src := range e.sources(sym) {
		rows, err := src.klines(ctx, e, sym, interval, limit)
		if err != nil || len(rows) == 0 {
			if err != nil {
				last = err
			}
			continue
		}
		return rows, nil
	}
	if last == nil {
		last = fmt.Errorf("no klines for %s", sym)
	}
	return nil, last
}

func (e *extFeed) fetchTrades(ctx context.Context, symbol string, limit int) ([]tradeDTO, error) {
	sym := strings.ToUpper(symbol)
	var last error
	for _, src := range e.sources(sym) {
		rows, err := src.trades(ctx, e, sym, limit)
		if err != nil || len(rows) == 0 {
			if err != nil {
				last = err
			}
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Time > rows[j].Time })
		return rows, nil
	}
	if last == nil {
		last = fmt.Errorf("no trades for %s", sym)
	}
	return nil, last
}

type extSource struct {
	name   string
	klines func(ctx context.Context, e *extFeed, symbol, interval string, limit int) ([]restKline, error)
	trades func(ctx context.Context, e *extFeed, symbol string, limit int) ([]tradeDTO, error)
}

func (e *extFeed) sources(symbol string) []extSource {
	fapiSym := symbol
	if alt := extFAPIAlias[symbol]; alt != "" {
		fapiSym = alt
	}
	return []extSource{
		{name: "binance", klines: func(ctx context.Context, e *extFeed, symbol, interval string, limit int) ([]restKline, error) {
			u := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d", e.binance, url.QueryEscape(symbol), interval, limit)
			return e.binanceKlines(ctx, u)
		}, trades: func(ctx context.Context, e *extFeed, symbol string, limit int) ([]tradeDTO, error) {
			u := fmt.Sprintf("%s/api/v3/aggTrades?symbol=%s&limit=%d", e.binance, url.QueryEscape(symbol), limit)
			return e.binanceTrades(ctx, u)
		}},
		{name: "binance-fapi", klines: func(ctx context.Context, e *extFeed, _, interval string, limit int) ([]restKline, error) {
			u := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d", e.fapi, url.QueryEscape(fapiSym), interval, limit)
			return e.binanceKlines(ctx, u)
		}, trades: func(ctx context.Context, e *extFeed, _ string, limit int) ([]tradeDTO, error) {
			u := fmt.Sprintf("%s/fapi/v1/aggTrades?symbol=%s&limit=%d", e.fapi, url.QueryEscape(fapiSym), limit)
			return e.binanceTrades(ctx, u)
		}},
		{name: "okx", klines: func(ctx context.Context, e *extFeed, symbol, interval string, limit int) ([]restKline, error) {
			return e.okxKlines(ctx, okxInst(symbol, false), interval, limit)
		}, trades: func(ctx context.Context, e *extFeed, symbol string, limit int) ([]tradeDTO, error) {
			return e.okxTrades(ctx, okxInst(symbol, false), limit)
		}},
		{name: "okx-swap", klines: func(ctx context.Context, e *extFeed, symbol, interval string, limit int) ([]restKline, error) {
			inst := okxInst(fapiSym, true)
			return e.okxKlines(ctx, inst, interval, limit)
		}, trades: func(ctx context.Context, e *extFeed, _ string, limit int) ([]tradeDTO, error) {
			return e.okxTrades(ctx, okxInst(fapiSym, true), limit)
		}},
	}
}

func okxInst(symbol string, swap bool) string {
	symbol = strings.ToUpper(symbol)
	base := symbol
	if strings.HasSuffix(symbol, "USDT") {
		base = strings.TrimSuffix(symbol, "USDT") + "-USDT"
	}
	if swap {
		return base + "-SWAP"
	}
	return base
}

func okxBar(interval string) string {
	switch interval {
	case "1h", "2h", "4h", "6h", "12h":
		return strings.ToUpper(interval)
	case "1d":
		return "1D"
	case "1w":
		return "1W"
	default:
		return interval
	}
}

func (e *extFeed) binanceKlines(ctx context.Context, u string) ([]restKline, error) {
	var rows [][]json.RawMessage
	if err := e.getJSON(ctx, u, &rows); err != nil {
		return nil, err
	}
	return parseExtBinanceKlines(rows), nil
}

func (e *extFeed) binanceTrades(ctx context.Context, u string) ([]tradeDTO, error) {
	var rows []struct {
		A int64  `json:"a"`
		P string `json:"p"`
		Q string `json:"q"`
		T int64  `json:"T"`
		M bool   `json:"m"`
	}
	if err := e.getJSON(ctx, u, &rows); err != nil {
		return nil, err
	}
	out := make([]tradeDTO, 0, len(rows))
	for _, r := range rows {
		if r.P == "" || r.Q == "" {
			continue
		}
		side := "buy"
		if r.M {
			side = "sell"
		}
		out = append(out, tradeDTO{TradeID: fmt.Sprint(r.A), Price: r.P, Quantity: r.Q, Side: side, Time: r.T})
	}
	return out, nil
}

func (e *extFeed) okxKlines(ctx context.Context, inst, interval string, limit int) ([]restKline, error) {
	if limit > 300 {
		limit = 300
	}
	u := fmt.Sprintf("%s/api/v5/market/candles?instId=%s&bar=%s&limit=%d", e.okx, url.QueryEscape(inst), okxBar(interval), limit)
	var body struct {
		Code string     `json:"code"`
		Data [][]string `json:"data"`
	}
	if err := e.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	if body.Code != "" && body.Code != "0" {
		return nil, fmt.Errorf("okx %s", body.Code)
	}
	return parseExtOKXKlines(body.Data), nil
}

func (e *extFeed) okxTrades(ctx context.Context, inst string, limit int) ([]tradeDTO, error) {
	if limit > 100 {
		limit = 100
	}
	u := fmt.Sprintf("%s/api/v5/market/trades?instId=%s&limit=%d", e.okx, url.QueryEscape(inst), limit)
	var body struct {
		Code string `json:"code"`
		Data []struct {
			TradeID string `json:"tradeId"`
			Px      string `json:"px"`
			Sz      string `json:"sz"`
			Side    string `json:"side"`
			Ts      string `json:"ts"`
		} `json:"data"`
	}
	if err := e.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	if body.Code != "" && body.Code != "0" {
		return nil, fmt.Errorf("okx %s", body.Code)
	}
	out := make([]tradeDTO, 0, len(body.Data))
	for _, r := range body.Data {
		ts, _ := decimal.NewFromString(r.Ts)
		side := strings.ToLower(r.Side)
		if side != "buy" && side != "sell" {
			side = "buy"
		}
		out = append(out, tradeDTO{TradeID: r.TradeID, Price: r.Px, Quantity: r.Sz, Side: side, Time: ts.IntPart()})
	}
	return out, nil
}

func parseExtBinanceKlines(rows [][]json.RawMessage) []restKline {
	out := make([]restKline, 0, len(rows))
	for _, r := range rows {
		if len(r) < 6 {
			continue
		}
		var ot int64
		if json.Unmarshal(r[0], &ot) != nil {
			continue
		}
		nums := make([]string, 5)
		ok := true
		for i := 0; i < 5; i++ {
			if json.Unmarshal(r[i+1], &nums[i]) != nil {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		out = append(out, restKline{Time: ot / 1000, Open: nums[0], High: nums[1], Low: nums[2], Close: nums[3], Volume: nums[4]})
	}
	return out
}

func parseExtOKXKlines(rows [][]string) []restKline {
	out := make([]restKline, 0, len(rows))
	for _, r := range rows {
		if len(r) < 6 {
			continue
		}
		ts, err := decimal.NewFromString(r[0])
		if err != nil {
			continue
		}
		out = append(out, restKline{Time: ts.IntPart() / 1000, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}

func (e *extFeed) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "linbit-md/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 180))
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}
