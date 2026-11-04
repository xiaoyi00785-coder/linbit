package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// feed.go: external index prices. The index for a symbol (e.g. BTCUSDT) is the
// latest spot price from an external public market-data source: Binance first,
// then OKX, Bybit and Gate.io as backups (INDEX_FEED order = failover order; the
// active source has weight 1). All are polled over REST (public endpoints, no
// API key). A quote is only usable when:
//   - the venue lists the symbol as trading (Binance status TRADING, OKX state
//     live, Bybit status Trading, Gate trade_status tradable): last-price
//     tickers keep answering for halted pairs, e.g. Binance TONUSDT (BREAK);
//   - it was fetched within IndexStale; and
//   - its price changed within IndexUnchangedStale (a venue that keeps serving
//     the same frozen price is treated as stale; INDEX_UNCHANGED_EXEMPT lists
//     pegged pairs that legitimately stay flat).
// When every source is unusable the index is unavailable and callers (futures
// mark price, KYC/admin USD conversion, market maker) fail closed.
//
// INDEX_FEED=off disables the external feed and serves the local spot last
// price instead (dev/tests only; refused in production).

type indexQuote struct {
	price     decimal.Decimal
	at        time.Time // last successful fetch containing the symbol
	changedAt time.Time // last time the price differed from the previous fetch
}

type indexSource struct {
	name    string
	fetch   func(ctx context.Context) (map[string]decimal.Decimal, error)
	mu      sync.Mutex
	quotes  map[string]indexQuote
	lastOK  time.Time
	lastErr string
}

// IndexFeed polls the configured sources and answers index lookups.
type IndexFeed struct {
	sources []*indexSource
	// tradfi reference sources (Binance TradFi perps first, OKX SWAP backup),
	// keyed by the venue's own instrument id; used only for symbols in tradfi.
	tradfiSources []*indexSource
	tradfi        map[string]tradfiRef // spot/futures symbol -> venue ids
	stale         time.Duration
	poll          time.Duration
	// unchangedStale: a quote whose price has not changed for this long is not
	// fresh (0 = guard off). unchangedExempt symbols (pegged pairs) skip it.
	unchangedStale  time.Duration
	unchangedExempt map[string]bool
	local           func(symbol string) (decimal.Decimal, bool) // INDEX_FEED=off fallback
	devMode         bool

	mu       sync.Mutex
	override map[string]decimal.Decimal // dev: fixed index per symbol
	outage   bool                       // dev: simulate every source down
}

var feedHTTP = &http.Client{Timeout: 4 * time.Second}

// NewIndexFeed builds the feed from config. names is the ordered source list.
func NewIndexFeed(cfg Config, local func(string) (decimal.Decimal, bool)) *IndexFeed {
	f := &IndexFeed{stale: cfg.IndexStale, poll: cfg.IndexPoll, local: local, devMode: cfg.DevMode,
		unchangedStale: cfg.IndexUnchangedStale, unchangedExempt: map[string]bool{},
		override: map[string]decimal.Decimal{}, tradfi: map[string]tradfiRef{}}
	for _, sym := range cfg.IndexUnchangedExempt {
		f.unchangedExempt[strings.ToUpper(sym)] = true
	}
	for _, n := range cfg.IndexFeed {
		switch n {
		case "binance":
			fbase := cfg.BinanceFAPIURL
			f.tradfiSources = append(f.tradfiSources, &indexSource{name: "binance-tradfi", quotes: map[string]indexQuote{},
				fetch: func(ctx context.Context) (map[string]decimal.Decimal, error) {
					if !f.hasTradFi() {
						return map[string]decimal.Decimal{}, nil
					}
					return fetchBinanceFAPI(ctx, fbase)
				}})
			base := cfg.BinanceURL
			st := newStatusCache(cfg.IndexStatusRefresh, binanceTrading(base))
			f.sources = append(f.sources, &indexSource{name: "binance", quotes: map[string]indexQuote{},
				fetch: withStatus(func(ctx context.Context) (map[string]decimal.Decimal, error) { return fetchBinance(ctx, base) }, st)})
		case "okx":
			obase := cfg.OKXURL
			f.tradfiSources = append(f.tradfiSources, &indexSource{name: "okx-swap", quotes: map[string]indexQuote{},
				fetch: func(ctx context.Context) (map[string]decimal.Decimal, error) {
					if !f.hasTradFi() {
						return map[string]decimal.Decimal{}, nil
					}
					return fetchOKXSwap(ctx, obase)
				}})
			base := cfg.OKXURL
			st := newStatusCache(cfg.IndexStatusRefresh, okxTrading(base))
			f.sources = append(f.sources, &indexSource{name: "okx", quotes: map[string]indexQuote{},
				fetch: withStatus(func(ctx context.Context) (map[string]decimal.Decimal, error) { return fetchOKX(ctx, base) }, st)})
		case "bybit":
			base := cfg.BybitURL
			st := newStatusCache(cfg.IndexStatusRefresh, bybitTrading(base))
			f.sources = append(f.sources, &indexSource{name: "bybit", quotes: map[string]indexQuote{},
				fetch: withStatus(func(ctx context.Context) (map[string]decimal.Decimal, error) { return fetchBybit(ctx, base) }, st)})
		case "gateio":
			base := cfg.GateURL
			st := newStatusCache(cfg.IndexStatusRefresh, gateTrading(base))
			f.sources = append(f.sources, &indexSource{name: "gateio", quotes: map[string]indexQuote{},
				fetch: withStatus(func(ctx context.Context) (map[string]decimal.Decimal, error) { return fetchGate(ctx, base) }, st)})
		}
	}
	return f
}

// External reports whether an external source is configured.
func (f *IndexFeed) External() bool { return len(f.sources) > 0 }

// Start launches one poller per source.
func (f *IndexFeed) Start(ctx context.Context) {
	for _, src := range f.sources {
		go f.run(ctx, src)
	}
	for _, src := range f.tradfiSources {
		go f.run(ctx, src)
	}
}

func (f *IndexFeed) run(ctx context.Context, src *indexSource) {
	t := time.NewTicker(f.poll)
	defer t.Stop()
	failing := false
	for {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		prices, err := src.fetch(cctx)
		cancel()
		now := time.Now()
		src.mu.Lock()
		if err != nil {
			src.lastErr = err.Error()
			if !failing {
				log.Printf("index feed %s: %v", src.name, err)
			}
			failing = true
		} else {
			if failing {
				log.Printf("index feed %s recovered", src.name)
			}
			failing = false
			src.lastErr = ""
			src.lastOK = now
			for sym, p := range prices {
				if !p.IsPositive() {
					continue
				}
				q := indexQuote{price: p, at: now, changedAt: now}
				if old, ok := src.quotes[sym]; ok && old.price.Equal(p) {
					q.changedAt = old.changedAt
				}
				src.quotes[sym] = q
			}
			// a symbol the venue stopped quoting (halted / delisted) must not
			// linger as a quote
			for sym := range src.quotes {
				if _, ok := prices[sym]; !ok {
					delete(src.quotes, sym)
				}
			}
		}
		src.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// IndexResult is one index lookup.
type IndexResult struct {
	Symbol     string           `json:"symbol"`
	Price      string           `json:"indexPrice"`
	Source     string           `json:"source"`
	UpdatedAt  time.Time        `json:"updatedAt"`
	Components []IndexComponent `json:"components,omitempty"`
}

// IndexComponent describes one constituent exchange of an index. The index
// uses primary/backup failover, so the active source has weight 1 and the
// others 0.
type IndexComponent struct {
	Exchange  string `json:"exchange"`
	Price     string `json:"price,omitempty"`
	Weight    string `json:"weight"`
	Fresh     bool   `json:"fresh"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// quoteFresh reports whether q is usable for id (the venue's key; the spot
// symbol for non-tradfi): fetched within stale and, unless the guard is off or
// id is exempt, with a price that changed within unchangedStale.
func (f *IndexFeed) quoteFresh(id string, q indexQuote, now time.Time) bool {
	if now.Sub(q.at) > f.stale {
		return false
	}
	if f.unchangedStale > 0 && !f.unchangedExempt[strings.ToUpper(id)] && !q.changedAt.IsZero() && now.Sub(q.changedAt) > f.unchangedStale {
		return false
	}
	return true
}

// Components lists every source quoting symbol, marking active as weight 1.
func (f *IndexFeed) Components(symbol, active string) []IndexComponent {
	symbol = strings.ToUpper(symbol)
	f.mu.Lock()
	ref, isTradFi := f.tradfi[symbol]
	f.mu.Unlock()
	srcs := f.sources
	if isTradFi {
		srcs = f.tradfiSources
	}
	now := time.Now()
	out := []IndexComponent{}
	for _, src := range srcs {
		id := symbol
		if isTradFi {
			id = ref.Binance
			if src.name == "okx-swap" {
				id = ref.OKX
			}
		}
		c := IndexComponent{Exchange: src.name, Weight: "0"}
		if src.name == active {
			c.Weight = "1"
		}
		src.mu.Lock()
		q, ok := src.quotes[id]
		src.mu.Unlock()
		if ok && id != "" {
			c.Price, c.Fresh, c.UpdatedAt = q.price.String(), f.quoteFresh(id, q, now), q.at.UnixMilli()
		}
		out = append(out, c)
	}
	return out
}

// Index returns the freshest usable index for symbol, honouring source order.
func (f *IndexFeed) Index(symbol string) (IndexResult, bool) {
	return f.index(symbol, true)
}

// IndexNoLocal is Index without the INDEX_FEED=off local-last-price fallback.
// Safe to call while holding the aggregator lock (the local fallback re-enters
// the aggregator).
func (f *IndexFeed) IndexNoLocal(symbol string) (IndexResult, bool) {
	return f.index(symbol, false)
}

func (f *IndexFeed) index(symbol string, allowLocal bool) (IndexResult, bool) {
	symbol = strings.ToUpper(symbol)
	f.mu.Lock()
	ov, hasOv := f.override[symbol]
	outage := f.outage
	f.mu.Unlock()
	if outage {
		return IndexResult{}, false
	}
	if hasOv {
		return IndexResult{Symbol: symbol, Price: ov.String(), Source: "dev-override", UpdatedAt: time.Now().UTC()}, true
	}
	f.mu.Lock()
	ref, isTradFi := f.tradfi[symbol]
	f.mu.Unlock()
	if !f.External() {
		if allowLocal && f.local != nil {
			if p, ok := f.local(symbol); ok && p.IsPositive() {
				return IndexResult{Symbol: symbol, Price: p.String(), Source: "local", UpdatedAt: time.Now().UTC()}, true
			}
		}
		return IndexResult{}, false
	}
	now := time.Now()
	if isTradFi {
		// tokenized stock / metal: reference = TradFi perp price, never the
		// spot feed (the venue's spot PAXG/XAUT is not the reference)
		for _, src := range f.tradfiSources {
			id := ref.Binance
			if src.name == "okx-swap" {
				id = ref.OKX
			}
			if id == "" {
				continue
			}
			src.mu.Lock()
			q, ok := src.quotes[id]
			src.mu.Unlock()
			if ok && f.quoteFresh(id, q, now) {
				return IndexResult{Symbol: symbol, Price: q.price.String(), Source: src.name, UpdatedAt: q.at.UTC()}, true
			}
		}
		return IndexResult{}, false
	}
	for _, src := range f.sources {
		src.mu.Lock()
		q, ok := src.quotes[symbol]
		src.mu.Unlock()
		if ok && f.quoteFresh(symbol, q, now) {
			return IndexResult{Symbol: symbol, Price: q.price.String(), Source: src.name, UpdatedAt: q.at.UTC()}, true
		}
	}
	return IndexResult{}, false
}

// Status summarises each source for /market/index without a symbol.
func (f *IndexFeed) Status() map[string]any {
	srcs := []map[string]any{}
	for _, src := range append(append([]*indexSource{}, f.sources...), f.tradfiSources...) {
		src.mu.Lock()
		row := map[string]any{"name": src.name, "healthy": !src.lastOK.IsZero() && time.Since(src.lastOK) <= f.stale, "symbols": len(src.quotes)}
		if !src.lastOK.IsZero() {
			row["lastOkAt"] = src.lastOK.UTC()
		}
		if src.lastErr != "" {
			row["lastError"] = src.lastErr
		}
		src.mu.Unlock()
		srcs = append(srcs, row)
	}
	f.mu.Lock()
	outage := f.outage
	f.mu.Unlock()
	mode := "external"
	if !f.External() {
		mode = "local"
	}
	return map[string]any{"mode": mode, "staleSeconds": int(f.stale / time.Second), "unchangedStaleSeconds": int(f.unchangedStale / time.Second), "simulatedOutage": outage, "sources": srcs}
}

// SetOverride (dev) pins an index; a zero price clears it.
func (f *IndexFeed) SetOverride(symbol string, p decimal.Decimal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.IsPositive() {
		f.override[strings.ToUpper(symbol)] = p
	} else {
		delete(f.override, strings.ToUpper(symbol))
	}
}

// SetOutage (dev) simulates every source down.
func (f *IndexFeed) SetOutage(on bool) { f.mu.Lock(); f.outage = on; f.mu.Unlock() }

// ---- sources ----

func getJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := feedHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v)
}

// fetchBinance reads all spot prices: GET /api/v3/ticker/price.
func fetchBinance(ctx context.Context, base string) (map[string]decimal.Decimal, error) {
	var rows []struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/api/v3/ticker/price", &rows); err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(rows))
	for _, r := range rows {
		if p, err := decimal.NewFromString(r.Price); err == nil {
			out[r.Symbol] = p
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return out, nil
}

// fetchOKX reads all spot tickers: GET /api/v5/market/tickers?instType=SPOT.
// instId BTC-USDT maps to BTCUSDT.
func fetchOKX(ctx context.Context, base string) (map[string]decimal.Decimal, error) {
	var body struct {
		Code string `json:"code"`
		Data []struct {
			InstID string `json:"instId"`
			Last   string `json:"last"`
		} `json:"data"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/api/v5/market/tickers?instType=SPOT", &body); err != nil {
		return nil, err
	}
	if body.Code != "0" {
		return nil, fmt.Errorf("okx code %s", body.Code)
	}
	out := make(map[string]decimal.Decimal, len(body.Data))
	for _, r := range body.Data {
		if p, err := decimal.NewFromString(r.Last); err == nil {
			out[strings.ReplaceAll(r.InstID, "-", "")] = p
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return out, nil
}

// tradfiRef maps a listed symbol to its reference instruments.
type tradfiRef struct{ Binance, OKX string }

func (f *IndexFeed) hasTradFi() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.tradfi) > 0 }

// SetTradFi replaces the tradfi symbol -> reference map.
func (f *IndexFeed) SetTradFi(m map[string]tradfiRef) { f.mu.Lock(); f.tradfi = m; f.mu.Unlock() }

// fetchBinanceFAPI reads all USDⓈ-M perp prices (incl. TradFi perps such as
// TSLAUSDT, XAUUSDT, XAGUSDT, CLUSDT): GET /fapi/v1/ticker/price.
func fetchBinanceFAPI(ctx context.Context, base string) (map[string]decimal.Decimal, error) {
	var rows []struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/fapi/v1/ticker/price", &rows); err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(rows))
	for _, r := range rows {
		if p, err := decimal.NewFromString(r.Price); err == nil {
			out[r.Symbol] = p
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return out, nil
}

// fetchOKXSwap reads all perpetual tickers: GET /api/v5/market/tickers?instType=SWAP,
// keyed by instId (e.g. AAPL-USDT-SWAP, XAU-USDT-SWAP, CL-USDT-SWAP).
func fetchOKXSwap(ctx context.Context, base string) (map[string]decimal.Decimal, error) {
	var body struct {
		Code string `json:"code"`
		Data []struct {
			InstID string `json:"instId"`
			Last   string `json:"last"`
		} `json:"data"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/api/v5/market/tickers?instType=SWAP", &body); err != nil {
		return nil, err
	}
	if body.Code != "0" {
		return nil, fmt.Errorf("okx code %s", body.Code)
	}
	out := make(map[string]decimal.Decimal, len(body.Data))
	for _, r := range body.Data {
		if p, err := decimal.NewFromString(r.Last); err == nil {
			out[r.InstID] = p
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return out, nil
}

// ---- HTTP ----

// handleIndex: GET /market/index?symbol=BTCUSDT -> IndexResult | 503.
// Without symbol it returns the feed status.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if sym == "" {
		common.WriteJSON(w, http.StatusOK, s.feed.Status())
		return
	}
	res, ok := s.feed.Index(sym)
	if !ok {
		// Same APIError envelope plus the per-source view (every component is
		// fresh:false or absent), so callers can see why the index is down.
		common.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "INDEX_UNAVAILABLE", "message": "外部指数价格不可用",
			"requestId": common.RequestID(), "fresh": false, "components": s.feed.Components(sym, "")})
		return
	}
	res.Components = s.feed.Components(sym, res.Source)
	if s.tradfi != nil {
		if d, ok := s.tradfi.deviation(sym); ok {
			common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": res.Symbol, "indexPrice": res.Price, "source": res.Source,
				"updatedAt": res.UpdatedAt, "components": res.Components, "reference": "tradfi", "deviationBps": d.DeviationBps, "widened": d.Widened, "widenFactor": d.WidenFactor})
			return
		}
	}
	common.WriteJSON(w, http.StatusOK, res)
}

// handleDevIndex (DEV_MODE): POST /internal/dev/index {symbol, price} pins an
// index ("0" clears); {outage:true|false} simulates a feed outage.
func (s *Server) handleDevIndex(w http.ResponseWriter, r *http.Request) {
	if s.cfg.InternalToken != "" && r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var req struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
		Outage *bool  `json:"outage"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	if req.Outage != nil {
		s.feed.SetOutage(*req.Outage)
	}
	if req.Symbol != "" {
		p, err := decimal.NewFromString(strings.TrimSpace(req.Price))
		if err != nil || p.IsNegative() {
			common.WriteError(w, http.StatusBadRequest, "INVALID_PRICE", "价格不合法")
			return
		}
		s.feed.SetOverride(req.Symbol, p)
	}
	common.WriteJSON(w, http.StatusOK, s.feed.Status())
}
