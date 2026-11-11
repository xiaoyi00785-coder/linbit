package main

// 从行情服务读指数。/market/index 用外部 Binance/OKX；只有 INDEX_FEED=off 才退回本地最新价。拿不到价就失败关闭。

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

// MarketClient 超时 3 秒。baseURL 空或请求失败时 LastPrice 返回 ok=false，调用方不得用 0 当作指数。
type MarketClient struct {
	baseURL string
	http    *http.Client
}

// NewMarketClient 去掉 baseURL 末尾斜杠。空地址不会在这里报错，LastPrice 会返回不可用。
func NewMarketClient(baseURL string) *MarketClient {
	return &MarketClient{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 3 * time.Second}}
}

// LastPrice 读 {asset}USDT 的 /market/index。未知交易对、行情过期或服务不可达都返回 ok=false。
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

// IndexComponent is one index source as reported by market-data /market/index.
type IndexComponent struct {
	Exchange  string `json:"exchange"`
	Price     string `json:"price,omitempty"`
	Weight    string `json:"weight"`
	Fresh     bool   `json:"fresh"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// IndexInfo 是指数来源和成分交易所。拿不到时 IndexInfo 方法返回 ok=false，不编造成分。
type IndexInfo struct {
	Source     string           `json:"source"`
	Components []IndexComponent `json:"components"`
}

// IndexInfo 读 asset/USDT 的指数来源和成分。服务不可达或解码失败返回 ok=false。
func (c *MarketClient) IndexInfo(ctx context.Context, asset string) (IndexInfo, bool) {
	var out IndexInfo
	if c == nil || c.baseURL == "" {
		return out, false
	}
	u := fmt.Sprintf("%s/market/index?symbol=%s", c.baseURL, url.QueryEscape(strings.ToUpper(asset)+"USDT"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return out, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, false
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, false
	}
	return out, true
}

// Klines 读行情 K 线（薄现货用指数，历史由 Binance 回补）。只做合约图的指数兜底；未知交易对或不可达返回 nil。
func (c *MarketClient) Klines(ctx context.Context, symbol, interval string, limit int) ([]klineBar, error) {
	if c == nil || c.baseURL == "" {
		return nil, nil
	}
	u := fmt.Sprintf("%s/market/klines?symbol=%s&interval=%s&limit=%d", c.baseURL, url.QueryEscape(symbol), url.QueryEscape(interval), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("market klines: HTTP %d", resp.StatusCode)
	}
	var rows []struct {
		Time int64  `json:"time"`
		O    string `json:"open"`
		H    string `json:"high"`
		L    string `json:"low"`
		C    string `json:"close"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	out := make([]klineBar, 0, len(rows))
	for _, k := range rows {
		o, e1 := decimal.NewFromString(k.O)
		h, e2 := decimal.NewFromString(k.H)
		l, e3 := decimal.NewFromString(k.L)
		cl, e4 := decimal.NewFromString(k.C)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || cl.Sign() <= 0 {
			continue
		}
		t := k.Time
		if t > 1e12 {
			t /= 1000
		}
		out = append(out, klineBar{Open: t, O: o, H: h, L: l, C: cl})
	}
	return out, nil
}
