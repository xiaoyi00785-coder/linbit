package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// MarketClient reads real-time prices from the market-data service.
type MarketClient struct {
	baseURL string
	http    *http.Client
}

// NewMarketClient builds a MarketClient.
func NewMarketClient(baseURL string) *MarketClient {
	return &MarketClient{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 3 * time.Second}}
}

// LastPrice returns the index price of {asset}USDT from market-data's
// /market/index (external Binance/OKX feed; local last price only when
// INDEX_FEED=off). ok=false means no usable price (unknown pair, feed stale or
// the service unreachable) and callers fail closed.
func (c *MarketClient) LastPrice(ctx context.Context, asset string) (decimal.Decimal, bool) {
	if c == nil || c.baseURL == "" {
		return decimal.Zero, false
	}
	u := fmt.Sprintf("%s/market/index?symbol=%s", c.baseURL, url.QueryEscape(strings.ToUpper(asset)+"USDT"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return decimal.Zero, false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return decimal.Zero, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, false
	}
	var t struct {
		IndexPrice string `json:"indexPrice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return decimal.Zero, false
	}
	p, err := decimal.NewFromString(t.IndexPrice)
	if err != nil || p.Sign() <= 0 {
		return decimal.Zero, false
	}
	return p, true
}
