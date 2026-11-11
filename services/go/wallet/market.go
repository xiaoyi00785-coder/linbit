package main

// market.go：向 market-data 取 {ASSET}USDT 指数价，给提现额度和风控折 USDT。
// 没有可用价格时 ok=false，调用方失败关闭。

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

// MarketClient 读 market-data 的实时指数。
type MarketClient struct {
	baseURL string
	http    *http.Client
}

// NewMarketClient builds a MarketClient.
func NewMarketClient(baseURL string) *MarketClient {
	return &MarketClient{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 3 * time.Second}}
}

// LastPrice 取 {asset}USDT 的指数价，来自 /market/index（外部 Binance/OKX；只有 INDEX_FEED=off 才用本地最新价）。
// ok=false 表示没有可用价：未知交易对、行情过期或服务不可达。调用方应失败关闭。
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
