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

	"github.com/shopspring/decimal"
)

// feed_sources.go: trading-status filtering and the Bybit / Gate.io spot
// sources.
//
// Venue "last price" tickers keep answering for halted / delisted pairs (e.g.
// Binance TONUSDT is status BREAK yet /ticker/price still returns the last
// traded price). Every source therefore pairs its ticker call with a cached,
// periodically refreshed instruments call and only quotes symbols whose
// venue status is "trading".

// statusCache holds the set of tradable symbols (in our BTCUSDT form) for one
// venue. It is loaded synchronously on first use (so nothing is ever quoted
// unfiltered) and then refreshed in the background every ttl, so the slow,
// multi-MB instrument call never stalls the price poller.
type statusCache struct {
	ttl    time.Duration // refresh interval
	maxAge time.Duration // beyond this a failed refresh makes the source fail closed
	load   func(ctx context.Context) (map[string]bool, error)

	mu         sync.Mutex
	set        map[string]bool
	loadedAt   time.Time
	refreshing bool
	now        func() time.Time
	inline     bool // tests: refresh inline instead of in the background
}

func newStatusCache(ttl time.Duration, load func(ctx context.Context) (map[string]bool, error)) *statusCache {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &statusCache{ttl: ttl, maxAge: 4 * ttl, load: load, now: time.Now}
}

// refresh loads a new set under its own timeout (independent of the poller's).
func (c *statusCache) refresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	set, err := c.load(ctx)
	if err == nil && len(set) == 0 {
		err = fmt.Errorf("empty instrument list")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = false
	if err != nil {
		return err
	}
	c.set, c.loadedAt = set, c.now()
	return nil
}

// trading returns the tradable set. A failed refresh keeps serving the last
// good set until maxAge; with no usable set it returns an error so the caller
// quotes nothing instead of quoting unfiltered prices.
func (c *statusCache) trading() (map[string]bool, error) {
	c.mu.Lock()
	now := c.now()
	switch {
	case c.set == nil:
		c.refreshing = true
		c.mu.Unlock()
		if err := c.refresh(); err != nil {
			return nil, fmt.Errorf("instrument status: %w", err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.set, nil
	case now.Sub(c.loadedAt) < c.ttl:
		defer c.mu.Unlock()
		return c.set, nil
	}
	// expired: refresh (background unless already running)
	start := !c.refreshing
	c.refreshing = true
	inline := c.inline
	c.mu.Unlock()
	if start {
		if inline {
			_ = c.refresh()
		} else {
			go func() {
				if err := c.refresh(); err != nil {
					log.Printf("index feed instrument status refresh: %v", err)
				}
			}()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Sub(c.loadedAt) >= c.maxAge {
		return nil, fmt.Errorf("instrument status older than %s", c.maxAge)
	}
	return c.set, nil
}

// getJSONLimit is getJSON with a caller-chosen body cap (exchangeInfo is large).
func getJSONLimit(ctx context.Context, url string, limit int64, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := statusHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(v)
}

// statusHTTP has a longer timeout than feedHTTP: instrument lists are MBs.
var statusHTTP = &http.Client{Timeout: 20 * time.Second}

// binanceTrading lists TRADING spot symbols: GET /api/v3/exchangeInfo.
// symbolStatus=TRADING asks the venue to omit halted pairs; the status is
// still checked per row in case a mirror ignores the parameter.
func binanceTrading(base string) func(ctx context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		var body struct {
			Symbols []struct {
				Symbol string `json:"symbol"`
				Status string `json:"status"`
			} `json:"symbols"`
		}
		if err := getJSONLimit(ctx, strings.TrimRight(base, "/")+"/api/v3/exchangeInfo?symbolStatus=TRADING", 128<<20, &body); err != nil {
			return nil, err
		}
		out := make(map[string]bool, len(body.Symbols))
		for _, s := range body.Symbols {
			if s.Status == "TRADING" {
				out[s.Symbol] = true
			}
		}
		return out, nil
	}
}

// okxTrading lists live spot instruments: GET /api/v5/public/instruments?instType=SPOT
// (state = live | suspend | preopen | test). BTC-USDT maps to BTCUSDT.
func okxTrading(base string) func(ctx context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		var body struct {
			Code string `json:"code"`
			Data []struct {
				InstID string `json:"instId"`
				State  string `json:"state"`
			} `json:"data"`
		}
		if err := getJSONLimit(ctx, strings.TrimRight(base, "/")+"/api/v5/public/instruments?instType=SPOT", 32<<20, &body); err != nil {
			return nil, err
		}
		if body.Code != "0" {
			return nil, fmt.Errorf("okx code %s", body.Code)
		}
		out := make(map[string]bool, len(body.Data))
		for _, r := range body.Data {
			if r.State == "live" {
				out[strings.ReplaceAll(r.InstID, "-", "")] = true
			}
		}
		return out, nil
	}
}

// bybitTrading lists Trading spot symbols:
// GET /v5/market/instruments-info?category=spot (status = Trading | PreLaunch | Delisted ...).
func bybitTrading(base string) func(ctx context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		out := map[string]bool{}
		cursor := ""
		for page := 0; page < 20; page++ {
			var body struct {
				RetCode int    `json:"retCode"`
				RetMsg  string `json:"retMsg"`
				Result  struct {
					List []struct {
						Symbol string `json:"symbol"`
						Status string `json:"status"`
					} `json:"list"`
					NextPageCursor string `json:"nextPageCursor"`
				} `json:"result"`
			}
			u := strings.TrimRight(base, "/") + "/v5/market/instruments-info?category=spot&limit=1000"
			if cursor != "" {
				u += "&cursor=" + cursor
			}
			if err := getJSONLimit(ctx, u, 32<<20, &body); err != nil {
				return nil, err
			}
			if body.RetCode != 0 {
				return nil, fmt.Errorf("bybit retCode %d %s", body.RetCode, body.RetMsg)
			}
			for _, r := range body.Result.List {
				if r.Status == "Trading" {
					out[r.Symbol] = true
				}
			}
			cursor = body.Result.NextPageCursor
			if cursor == "" {
				break
			}
		}
		return out, nil
	}
}

// gateTrading lists tradable spot pairs: GET /api/v4/spot/currency_pairs
// (trade_status = tradable | untradable | buyable | sellable). BTC_USDT maps
// to BTCUSDT.
func gateTrading(base string) func(ctx context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		var rows []struct {
			ID          string `json:"id"`
			TradeStatus string `json:"trade_status"`
		}
		if err := getJSONLimit(ctx, strings.TrimRight(base, "/")+"/api/v4/spot/currency_pairs", 32<<20, &rows); err != nil {
			return nil, err
		}
		out := make(map[string]bool, len(rows))
		for _, r := range rows {
			if r.TradeStatus == "tradable" {
				out[strings.ReplaceAll(r.ID, "_", "")] = true
			}
		}
		return out, nil
	}
}

// onlyTrading drops every price whose symbol is not in the trading set.
func onlyTrading(prices map[string]decimal.Decimal, trading map[string]bool) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal, len(prices))
	for sym, p := range prices {
		if trading[sym] {
			out[sym] = p
		}
	}
	return out
}

// fetchBybit reads all spot last prices: GET /v5/market/tickers?category=spot.
func fetchBybit(ctx context.Context, base string) (map[string]decimal.Decimal, error) {
	var body struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol    string `json:"symbol"`
				LastPrice string `json:"lastPrice"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/v5/market/tickers?category=spot", &body); err != nil {
		return nil, err
	}
	if body.RetCode != 0 {
		return nil, fmt.Errorf("bybit retCode %d %s", body.RetCode, body.RetMsg)
	}
	out := make(map[string]decimal.Decimal, len(body.Result.List))
	for _, r := range body.Result.List {
		if p, err := decimal.NewFromString(r.LastPrice); err == nil {
			out[r.Symbol] = p
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return out, nil
}

// fetchGate reads all spot last prices: GET /api/v4/spot/tickers.
// currency_pair BTC_USDT maps to BTCUSDT.
func fetchGate(ctx context.Context, base string) (map[string]decimal.Decimal, error) {
	var rows []struct {
		Pair string `json:"currency_pair"`
		Last string `json:"last"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/api/v4/spot/tickers", &rows); err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(rows))
	for _, r := range rows {
		if p, err := decimal.NewFromString(r.Last); err == nil {
			out[strings.ReplaceAll(r.Pair, "_", "")] = p
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return out, nil
}

// withStatus wraps a price fetcher so only venue-TRADING symbols are returned.
func withStatus(price func(ctx context.Context) (map[string]decimal.Decimal, error), st *statusCache) func(ctx context.Context) (map[string]decimal.Decimal, error) {
	return func(ctx context.Context) (map[string]decimal.Decimal, error) {
		trading, err := st.trading()
		if err != nil {
			return nil, err
		}
		prices, err := price(ctx)
		if err != nil {
			return nil, err
		}
		out := onlyTrading(prices, trading)
		if len(out) == 0 {
			return nil, fmt.Errorf("no trading symbols in response")
		}
		return out, nil
	}
}
