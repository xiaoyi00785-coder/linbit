package main

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

// FeatureGate reads the same app feature registry that backs /app/features.
// Unknown keys, a missing row or a database error all keep the feature closed.
type FeatureGate struct {
	store  *Store
	direct map[string]string
}

func NewFeatureGate(cfg Config, store *Store) *FeatureGate {
	return &FeatureGate{store: store, direct: map[string]string{"paperTrading": cfg.PaperTradingEnabled, "priceAlerts": cfg.PriceAlertsEnabled}}
}
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

// MarketClient only uses the privileged IndexNoLocal contract, never the
// public /market/index endpoint (whose development mode can fall back to LAST).
type Market interface {
	TradingSymbol(context.Context, string) (Symbol, error)
	Symbol(context.Context, string) (Symbol, error)
	Index(context.Context, string) (Quote, error)
}
type NotificationSender interface {
	InApp(context.Context, string, string, string, string) (string, error)
}

type MarketClient struct {
	base      string
	token     string
	maxAge    time.Duration
	client    *http.Client
	mu        sync.Mutex
	symbols   map[string]Symbol
	refreshed time.Time
}

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

// Notifier delivers only in-app system notices; email is explicitly suppressed.
type Notifier struct {
	base, token string
	client      *http.Client
}

func NewNotifier(base, token string) *Notifier {
	return &Notifier{base: strings.TrimRight(base, "/"), token: token, client: &http.Client{Timeout: 5 * time.Second}}
}
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
