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
//   - POST /strategy/grid    {symbol}              grid creation
//   - POST /strategy/grid/square/{id}/copy         source bot symbol (strategy internal lookup)
//   - POST /convert/quote    {from,to}             both assets
//   - POST /convert/accept   {quoteId}             quote assets (asset internal lookup;
//                                                  404 = unknown quote, upstream rejects)
//   - POST /assets/transfer, /sub-accounts/transfer {asset}
//   Copy-trading mirrors are gated in futures (copy_tradfi.go), per follower.
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
	marketURL, kycURL, strategyURL, assetURL, token string
	ttl                                             time.Duration
	client                                          *http.Client

	mu     sync.Mutex
	gate   *tradfiGate
	gateAt time.Time
	kyc    map[string]kycEntry
}

func newTradFiGuard(cfg Config) *tradfiGuard {
	return &tradfiGuard{marketURL: strings.TrimRight(cfg.MarketURL, "/"), kycURL: strings.TrimRight(cfg.KycURL, "/"),
		strategyURL: strings.TrimRight(cfg.StrategyURL, "/"), assetURL: strings.TrimRight(cfg.AssetURL, "/"),
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

// lookup GETs an internal JSON object, returning the HTTP status (0 on transport error).
func (t *tradfiGuard) lookup(ctx context.Context, u string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	if t.token != "" {
		req.Header.Set("X-Internal-Token", t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
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

// peekField reads a top-level string field from a JSON body (upper-cased) and restores the body.
func peekField(r *http.Request, field string) string {
	return strings.ToUpper(peekRaw(r, field))
}

// peekRaw is peekField without case folding (ids).
func peekRaw(r *http.Request, field string) string {
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
	return strings.TrimSpace(s)
}

// target is one instrument a request touches: kind "any" (any TradFi flag),
// "spot" / "futures" (symbol maps) or "asset" (asset map).
type target struct{ kind, key string }

// classify returns (gated, targets). Lookups that need upstream state
// (grid copy source, convert quote) return kind "gridCopy" / "convertQuote"
// with the path id as key; check resolves them.
func classify(r *http.Request) (bool, []target) {
	p := r.URL.Path
	post := r.Method == http.MethodPost
	one := func(kind, key string) (bool, []target) { return true, []target{{kind, key}} }
	switch {
	case p == "/tradfi" || strings.HasPrefix(p, "/tradfi/"):
		return one("any", "")
	case post && p == "/spot/orders":
		return one("spot", peekField(r, "symbol"))
	case post && p == "/futures/orders":
		return one("futures", peekField(r, "symbol"))
	case r.Method == http.MethodGet && p == "/wallet/deposit/address":
		return one("asset", strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset"))))
	case post && p == "/wallet/withdraw":
		return one("asset", peekField(r, "asset"))
	// Entry paths beyond direct orders (stop / close / cancel stay open).
	case post && p == "/strategy/grid":
		return one("spot", peekField(r, "symbol"))
	case post && strings.HasPrefix(p, "/strategy/grid/square/") && strings.HasSuffix(p, "/copy"):
		return one("gridCopy", strings.TrimSuffix(strings.TrimPrefix(p, "/strategy/grid/square/"), "/copy"))
	case post && p == "/convert/quote":
		return true, []target{{"asset", peekField(r, "from")}, {"asset", peekField(r, "to")}}
	case post && p == "/convert/accept":
		return one("convertQuote", peekRaw(r, "quoteId"))
	case post && (p == "/assets/transfer" || p == "/sub-accounts/transfer"):
		return one("asset", peekField(r, "asset"))
	}
	return false, nil
}

// resolve turns lookup targets into instrument targets. found=false means the
// referenced object does not exist (upstream answers 404 itself).
func (t *tradfiGuard) resolve(ctx context.Context, tg target) (out []target, found bool, err error) {
	switch tg.kind {
	case "gridCopy":
		var b struct {
			Symbol string `json:"symbol"`
		}
		code, err := t.lookup(ctx, t.strategyURL+"/internal/strategy/grid/"+url.PathEscape(tg.key)+"/symbol", &b)
		if code == http.StatusNotFound {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return []target{{"spot", strings.ToUpper(b.Symbol)}}, true, nil
	case "convertQuote":
		var q struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		code, err := t.lookup(ctx, t.assetURL+"/internal/convert/quotes/"+url.PathEscape(tg.key), &q)
		if code == http.StatusNotFound {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return []target{{"asset", strings.ToUpper(q.From)}, {"asset", strings.ToUpper(q.To)}}, true, nil
	}
	return []target{tg}, true, nil
}

func writeUnavailable(w http.ResponseWriter) {
	common.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务暂不可用，请稍后重试")
}

// check returns false after writing the error response.
func (t *tradfiGuard) check(w http.ResponseWriter, r *http.Request) bool {
	gated, raw := classify(r)
	if !gated {
		return true
	}
	var targets []target
	for _, tg := range raw {
		if tg.kind != "any" && tg.key == "" {
			continue // upstream validates the missing field
		}
		rs, found, err := t.resolve(r.Context(), tg)
		if err != nil {
			writeUnavailable(w) // cannot tell whether it is TradFi: fail closed
			return false
		}
		if found {
			targets = append(targets, rs...)
		}
	}
	if len(targets) == 0 {
		return true
	}
	g, err := t.snapshot(r.Context())
	if err != nil {
		// No gate ever loaded: fail closed for the seeded TradFi instruments
		// only, so crypto trading does not depend on market-data being up.
		for _, tg := range targets {
			if tg.kind == "any" || seededTradFi[tg.key] {
				writeUnavailable(w)
				return false
			}
		}
		return true
	}
	hit := false
	for _, tg := range targets {
		var flag string
		switch tg.kind {
		case "any":
			if !g.Features["tokenizedStocks"] && !g.Features["metals"] {
				common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "该功能暂未开放")
				return false
			}
			hit = true
			continue
		case "spot":
			flag = g.SpotSymbols[tg.key]
		case "futures":
			flag = g.FuturesSymbols[tg.key]
		case "asset":
			flag = g.Assets[tg.key]
		}
		if flag == "" {
			continue // not a TradFi instrument
		}
		if !g.Features[flag] {
			common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "该功能暂未开放")
			return false
		}
		hit = true
	}
	if !hit {
		return true
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
			writeUnavailable(w)
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
