package main

// 模拟盘的外部只读依赖：功能开关、指数价、站内通知。指数不走会回落到 LAST 的公开接口。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// FeatureGate 读和 /app/features 同一张开关表。未知 key、缺行或数据库错误都保持关闭。
// 环境变量 PAPER_TRADING_ENABLED / PRICE_ALERTS_ENABLED 为 true/false 时压过库。
type FeatureGate struct {
	store  *Store
	direct map[string]string
}

// NewFeatureGate 只接 paperTrading 和 priceAlerts。其他 key 在 Enabled 里直接 false。
func NewFeatureGate(cfg Config, store *Store) *FeatureGate {
	return &FeatureGate{store: store, direct: map[string]string{"paperTrading": cfg.PaperTradingEnabled, "priceAlerts": cfg.PriceAlertsEnabled}}
}

// Enabled 对未知 key 返回 false 且 err=nil。库错误返回 err，调用方应按关闭处理。
func (g *FeatureGate) Enabled(ctx context.Context, key string) (bool, error) {
	if key != "paperTrading" && key != "priceAlerts" {
		return false, nil
	}
	if strings.EqualFold(g.direct[key], "false") {
		return false, nil
	}
	if strings.EqualFold(g.direct[key], "true") {
		return true, nil
	}
	var enabled bool
	err := g.store.db.QueryRowContext(ctx, `SELECT enabled FROM app_features WHERE key=$1`, key).Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

// Market 提供交易对和指数。Index 必须是外部指数，不能用现货最新成交顶上。
type Market interface {
	TradingSymbol(context.Context, string) (Symbol, error)
	Symbol(context.Context, string) (Symbol, error)
	Index(context.Context, string) (Quote, error)
}

// NotificationSender 只发站内信。邮件由实现显式关掉。
type NotificationSender interface {
	InApp(context.Context, string, string, string, string) (string, error)
}

// MarketClient 的指数走 GET /internal/index，不走公开 /market/index（开发模式会退回 LAST）。
type MarketClient struct {
	base      string
	token     string
	maxAge    time.Duration
	client    *http.Client
	mu        sync.Mutex
	symbols   map[string]Symbol
	refreshed time.Time
}

// NewMarketClient 超时 5 秒。maxAge 是指数允许的最大年龄，超过则 PRICE_STALE。
func NewMarketClient(base, token string, maxAge time.Duration) *MarketClient {
	return &MarketClient{base: strings.TrimRight(base, "/"), token: token, maxAge: maxAge, client: &http.Client{Timeout: 5 * time.Second}, symbols: map[string]Symbol{}}
}
func (m *MarketClient) do(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return err
	}
	if strings.HasPrefix(path, "/internal/") {
		req.Header.Set("X-Internal-Token", m.token)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		var e struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Code == "" {
			e.Code = "INDEX_UNAVAILABLE"
		}
		return apiErr(http.StatusServiceUnavailable, e.Code, "新鲜外部指数不可用")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("market status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}

// Index 只接受 source=INDEX 且符号匹配的报价。价格不是正数、时间为零、超前超过 5 秒或老于 maxAge 都拒绝。
func (m *MarketClient) Index(ctx context.Context, symbol string) (Quote, error) {
	var wire struct {
		Symbol    string    `json:"symbol"`
		Price     string    `json:"indexPrice"`
		Source    string    `json:"source"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := m.do(ctx, "/internal/index?symbol="+url.QueryEscape(symbol), &wire); err != nil {
		return Quote{}, err
	}
	p, err := decimal.NewFromString(wire.Price)
	if err != nil || !p.IsPositive() {
		return Quote{}, apiErr(http.StatusServiceUnavailable, "INVALID_PRICE", "外部指数价格无效")
	}
	q := Quote{Symbol: wire.Symbol, Price: p, Source: wire.Source, ObservedAt: wire.UpdatedAt}
	now := time.Now().UTC()
	if !strings.EqualFold(wire.Symbol, strings.ToUpper(strings.TrimSpace(symbol))) || !strings.EqualFold(wire.Source, "INDEX") {
		return Quote{}, apiErr(http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "报价源不匹配")
	}
	if !strings.EqualFold(wire.Symbol, strings.ToUpper(strings.TrimSpace(symbol))) || !strings.EqualFold(wire.Source, "INDEX") {
		return Quote{}, apiErr(http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "报价源不匹配")
	}
	if q.ObservedAt.IsZero() || q.ObservedAt.After(now.Add(5*time.Second)) || now.Sub(q.ObservedAt) > m.maxAge {
		return Quote{}, apiErr(http.StatusServiceUnavailable, "PRICE_STALE", "外部指数已过期")
	}
	return q, nil
}

// Symbol 缓存 /market/symbols 一分钟。不在列表里返回 UNKNOWN_SYMBOL，不造一个零精度交易对。
func (m *MarketClient) Symbol(ctx context.Context, symbol string) (Symbol, error) {
	symbol = strings.ToUpper(symbol)
	m.mu.Lock()
	cached, ok := m.symbols[symbol]
	m.mu.Unlock()
	if ok && time.Since(m.refreshed) < time.Minute {
		return cached, nil
	}
	var wire []struct {
		Symbol            string  `json:"symbol"`
		Base              string  `json:"base"`
		Quote             string  `json:"quote"`
		PricePrecision    int     `json:"pricePrecision"`
		QuantityPrecision int     `json:"quantityPrecision"`
		MinQuantity       *string `json:"minQuantity"`
		MinNotional       *string `json:"minNotional"`
		Status            string  `json:"status"`
	}
	if err := m.do(ctx, "/market/symbols", &wire); err != nil {
		return Symbol{}, err
	}
	next := map[string]Symbol{}
	for _, s := range wire {
		item := Symbol{Symbol: s.Symbol, Base: s.Base, Quote: s.Quote, PricePrecision: s.PricePrecision, QuantityPrecision: s.QuantityPrecision, MinQuantity: decimal.Zero, MinNotional: decimal.Zero, Status: s.Status}
		if s.MinQuantity != nil {
			item.MinQuantity, _ = decimal.NewFromString(*s.MinQuantity)
		}
		if s.MinNotional != nil {
			item.MinNotional, _ = decimal.NewFromString(*s.MinNotional)
		}
		next[item.Symbol] = item
	}
	m.mu.Lock()
	m.symbols = next
	m.refreshed = time.Now()
	m.refreshed = time.Now()
	m.mu.Unlock()
	info, ok := next[symbol]
	if !ok {
		return Symbol{}, apiErr(http.StatusBadRequest, "UNKNOWN_SYMBOL", "不支持的虚拟交易对")
	}
	return info, nil
}

// TradingSymbol 在 Symbol 之上要求 status=trading 且计价币是 USDT。暂停或其他计价币不能下模拟现货单。
func (m *MarketClient) TradingSymbol(ctx context.Context, symbol string) (Symbol, error) {
	s, err := m.Symbol(ctx, symbol)
	if err != nil {
		return Symbol{}, err
	}
	if s.Status != "trading" {
		return Symbol{}, apiErr(http.StatusBadRequest, "SYMBOL_PAUSED", "交易对当前暂停")
	}
	if s.Quote != "USDT" {
		return Symbol{}, apiErr(http.StatusBadRequest, "UNKNOWN_SYMBOL", "P0 仅支持 USDT 计价现货")
	}
	return s, nil
}

// Notifier 只发站内系统通知。请求里 email=false，不发邮件。
type Notifier struct {
	base, token string
	client      *http.Client
}

// NewNotifier 超时 5 秒。空地址会在发送时失败。
func NewNotifier(base, token string) *Notifier {
	return &Notifier{base: strings.TrimRight(base, "/"), token: token, client: &http.Client{Timeout: 5 * time.Second}}
}

// InApp 按 dedupKey 让通知服务去重。email 固定 false。
func (n *Notifier) InApp(ctx context.Context, uid, title, body, dedupKey string) (string, error) {
	payload := map[string]any{"uid": uid, "kind": "system", "title": title, "body": body, "email": false, "dedupKey": dedupKey}
	bodyJSON, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.base+"/internal/notifications", strings.NewReader(string(bodyJSON)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", n.token)
	resp, err := n.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("notification status %d", resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ID == "" {
		return "", fmt.Errorf("notification id missing")
	}
	return result.ID, nil
}

func requestHash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func isUnique(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23505"
}
