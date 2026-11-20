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

// MarketClient reads underlying index prices from the market-data service.
type MarketClient struct {
	baseURL string
	http    *http.Client
}

func NewMarketClient(baseURL string) *MarketClient {
	return &MarketClient{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 3 * time.Second}}
}

// IndexPrice returns the USDT index price of `asset` (BTC, ETH, SOL). ok=false
// means the price is unavailable; callers fail closed.
func (c *MarketClient) IndexPrice(ctx context.Context, asset string) (decimal.Decimal, bool) {
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
