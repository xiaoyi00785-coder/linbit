package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// tradfi.go: gate for tokenized stocks / metals (docs/tradfi.md).
//
// Gated requests:
//   - /tradfi/*                                    any TradFi flag on
//   - POST /spot/orders      {symbol} TradFi spot  flag of the symbol's kind
//   - POST /futures/orders   {symbol} XAG/WTI      metals
//   - GET  /wallet/deposit/address?asset=          flag of the asset's kind
//   - POST /wallet/withdraw  {asset}
//
// Checks, in order: feature flag off -> 403 FEATURE_DISABLED; CF-IPCountry in
// the restricted list -> 403 REGION_RESTRICTED; logged-in user whose KYC
// country is restricted -> 403 REGION_RESTRICTED. Lookups that fail (market-
// data gate, KYC country) fail closed with 503. Closing / reducing positions
// and cancelling orders are never gated so users can always exit.

type tradfiGate struct {
	Features            map[string]bool   `json:"features"`
	RestrictedCountries []string          `json:"restrictedCountries"`
	SpotSymbols         map[string]string `json:"spotSymbols"`
	FuturesSymbols      map[string]string `json:"futuresSymbols"`
	Assets              map[string]string `json:"assets"`
}

type kycEntry struct {
	country string
	at      time.Time
}

type tradfiGuard struct {
	marketURL, kycURL, token string
	ttl                      time.Duration
	client                   *http.Client

	mu     sync.Mutex
	gate   *tradfiGate
	gateAt time.Time
	kyc    map[string]kycEntry
}

func newTradFiGuard(cfg Config) *tradfiGuard {
	return &tradfiGuard{marketURL: strings.TrimRight(cfg.MarketURL, "/"), kycURL: strings.TrimRight(cfg.KycURL, "/"),
		token: cfg.InternalToken, ttl: 30 * time.Second, client: &http.Client{Timeout: 3 * time.Second}, kyc: map[string]kycEntry{}}
}

func (t *tradfiGuard) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if t.token != "" {
		req.Header.Set("X-Internal-Token", t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

func (t *tradfiGuard) snapshot(ctx context.Context) (*tradfiGate, error) {
	t.mu.Lock()
	if t.gate != nil && time.Since(t.gateAt) < t.ttl {
		g := t.gate
		t.mu.Unlock()
		return g, nil
	}
	t.mu.Unlock()
	var g tradfiGate
	if err := t.getJSON(ctx, t.marketURL+"/internal/tradfi/gate", &g); err != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.gate != nil {
			return t.gate, nil // market-data blip: keep the last known gate
		}
		return nil, err
	}
	t.mu.Lock()
	t.gate, t.gateAt = &g, time.Now()
	t.mu.Unlock()
	return &g, nil
}

func (t *tradfiGuard) kycCountry(ctx context.Context, uid string) (string, error) {
	t.mu.Lock()
	if e, ok := t.kyc[uid]; ok && time.Since(e.at) < t.ttl {
		t.mu.Unlock()
		return e.country, nil
	}
	t.mu.Unlock()
	var out struct {
		Country string `json:"country"`
	}
	if err := t.getJSON(ctx, t.kycURL+"/internal/kyc/country?userId="+url.QueryEscape(uid), &out); err != nil {
		return "", err
	}
	c := strings.ToUpper(strings.TrimSpace(out.Country))
	t.mu.Lock()
	if len(t.kyc) > 50000 {
		t.kyc = map[string]kycEntry{}
	}
	t.kyc[uid] = kycEntry{country: c, at: time.Now()}
	t.mu.Unlock()
	return c, nil
}

// peekField reads a top-level string field from a JSON body and restores the body.
func peekField(r *http.Request, field string) string {
	if r.Body == nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(b))
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	s, _ := m[field].(string)
	return strings.ToUpper(strings.TrimSpace(s))
}

// classify returns (gated, which map, key) for the request.
func classify(r *http.Request) (bool, string, string) {
	p := r.URL.Path
	switch {
	case p == "/tradfi" || strings.HasPrefix(p, "/tradfi/"):
		return true, "any", ""
	case r.Method == http.MethodPost && p == "/spot/orders":
		return true, "spot", peekField(r, "symbol")
	case r.Method == http.MethodPost && p == "/futures/orders":
		return true, "futures", peekField(r, "symbol")
	case r.Method == http.MethodGet && p == "/wallet/deposit/address":
		return true, "asset", strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	case r.Method == http.MethodPost && p == "/wallet/withdraw":
		return true, "asset", peekField(r, "asset")
	}
	return false, "", ""
}

// check returns false after writing the error response.
func (t *tradfiGuard) check(w http.ResponseWriter, r *http.Request) bool {
	gated, kind, key := classify(r)
	if !gated {
		return true
	}
	if kind != "any" && key == "" {
		return true // upstream validates the missing field
	}
	g, err := t.snapshot(r.Context())
	if err != nil {
		// No gate ever loaded: fail closed for the seeded TradFi instruments
		// only, so crypto trading does not depend on market-data being up.
		if kind == "any" || seededTradFi[key] {
			common.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务暂不可用，请稍后重试")
			return false
		}
		return true
	}
	var flag string
	switch kind {
	case "any":
		if !g.Features["tokenizedStocks"] && !g.Features["metals"] {
			common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "该功能暂未开放")
			return false
		}
	case "spot":
		flag = g.SpotSymbols[key]
	case "futures":
		flag = g.FuturesSymbols[key]
	case "asset":
		flag = g.Assets[key]
	}
	if kind != "any" {
		if flag == "" {
			return true // not a TradFi instrument
		}
		if !g.Features[flag] {
			common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "该功能暂未开放")
			return false
		}
	}
	restricted := map[string]bool{}
	for _, c := range g.RestrictedCountries {
		restricted[strings.ToUpper(c)] = true
	}
	if ip := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry"))); ip != "" && restricted[ip] {
		common.WriteError(w, http.StatusForbidden, "REGION_RESTRICTED", "您所在的国家或地区暂不支持该服务")
		return false
	}
	if uid := r.Header.Get("X-User-Id"); uid != "" {
		c, err := t.kycCountry(r.Context(), uid)
		if err != nil {
			common.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务暂不可用，请稍后重试")
			return false
		}
		if c != "" && restricted[c] {
			common.WriteError(w, http.StatusForbidden, "REGION_RESTRICTED", "您所在的国家或地区暂不支持该服务")
			return false
		}
	}
	return true
}

// seededTradFi: instruments from market-data migration 00010 (fallback only).
var seededTradFi = func() map[string]bool {
	m := map[string]bool{}
	for _, a := range strings.Split("AAPL,TSLA,NVDA,MSFT,GOOGL,AMZN,META,COIN,SPY,PAXG,XAUT,XAG,WTI", ",") {
		m[a], m[a+"USDT"] = true, true
	}
	return m
}()
