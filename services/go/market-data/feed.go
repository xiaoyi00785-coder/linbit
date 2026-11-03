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
// OKX as backup. Both are polled over REST (public endpoints, no API key).
// A source quote older than IndexStale is ignored; when every source is stale
// the index is unavailable and callers (futures mark price, KYC/admin USD
// conversion, market maker) fail closed.
//
// INDEX_FEED=off disables the external feed and serves the local spot last
// price instead (dev/tests only; refused in production).

type indexQuote struct {
	price decimal.Decimal
	at    time.Time
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
	stale   time.Duration
	poll    time.Duration
	local   func(symbol string) (decimal.Decimal, bool) // INDEX_FEED=off fallback
	devMode bool

	mu       sync.Mutex
	override map[string]decimal.Decimal // dev: fixed index per symbol
	outage   bool                       // dev: simulate every source down
}

var feedHTTP = &http.Client{Timeout: 4 * time.Second}

// NewIndexFeed builds the feed from config. names is the ordered source list.
func NewIndexFeed(cfg Config, local func(string) (decimal.Decimal, bool)) *IndexFeed {
	f := &IndexFeed{stale: cfg.IndexStale, poll: cfg.IndexPoll, local: local, devMode: cfg.DevMode,
		override: map[string]decimal.Decimal{}}
	for _, n := range cfg.IndexFeed {
		switch n {
		case "binance":
			base := cfg.BinanceURL
			f.sources = append(f.sources, &indexSource{name: "binance", quotes: map[string]indexQuote{},
				fetch: func(ctx context.Context) (map[string]decimal.Decimal, error) { return fetchBinance(ctx, base) }})
		case "okx":
			base := cfg.OKXURL
			f.sources = append(f.sources, &indexSource{name: "okx", quotes: map[string]indexQuote{},
				fetch: func(ctx context.Context) (map[string]decimal.Decimal, error) { return fetchOKX(ctx, base) }})
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
				if p.IsPositive() {
					src.quotes[sym] = indexQuote{price: p, at: now}
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
	Symbol    string    `json:"symbol"`
	Price     string    `json:"indexPrice"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	if !f.External() {
		if allowLocal && f.local != nil {
			if p, ok := f.local(symbol); ok && p.IsPositive() {
				return IndexResult{Symbol: symbol, Price: p.String(), Source: "local", UpdatedAt: time.Now().UTC()}, true
			}
		}
		return IndexResult{}, false
	}
	now := time.Now()
	for _, src := range f.sources {
		src.mu.Lock()
		q, ok := src.quotes[symbol]
		src.mu.Unlock()
		if ok && now.Sub(q.at) <= f.stale {
			return IndexResult{Symbol: symbol, Price: q.price.String(), Source: src.name, UpdatedAt: q.at.UTC()}, true
		}
	}
	return IndexResult{}, false
}

// Status summarises each source for /market/index without a symbol.
func (f *IndexFeed) Status() map[string]any {
	srcs := []map[string]any{}
	for _, src := range f.sources {
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
	return map[string]any{"mode": mode, "staleSeconds": int(f.stale / time.Second), "simulatedOutage": outage, "sources": srcs}
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
		common.WriteError(w, http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "外部指数价格不可用")
		return
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
