package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// copy_tradfi.go: TradFi gate for copy-trading mirrors (docs/tradfi.md).
//
// Mirrored opens happen server-side, so the gateway gate never sees them. Before
// opening / adding a TradFi perp (XAG, WTI, ...) for a follower we check the
// same market-data gate the gateway uses: flag off -> FEATURE_DISABLED,
// follower KYC country restricted -> REGION_RESTRICTED, gate or KYC lookup
// failing -> TRADFI_UNAVAILABLE (fail closed). The skip is recorded in
// fut_copy_trades. Reduces / closes are never gated.

type copyGate struct {
	Features            map[string]bool   `json:"features"`
	RestrictedCountries []string          `json:"restrictedCountries"`
	FuturesSymbols      map[string]string `json:"futuresSymbols"`
}

type copyTradFi struct {
	marketURL, kycURL, token string
	ttl                      time.Duration
	client                   *http.Client

	mu     sync.Mutex
	gate   *copyGate
	gateAt time.Time
	kyc    map[string]copyKyc
}

type copyKyc struct {
	country string
	at      time.Time
}

func newCopyTradFi(marketURL, kycURL, token string) *copyTradFi {
	return &copyTradFi{marketURL: strings.TrimRight(marketURL, "/"), kycURL: strings.TrimRight(kycURL, "/"), token: token,
		ttl: 30 * time.Second, client: &http.Client{Timeout: 3 * time.Second}, kyc: map[string]copyKyc{}}
}

func (t *copyTradFi) get(ctx context.Context, u string, v any) error {
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

func (t *copyTradFi) snapshot(ctx context.Context) (*copyGate, error) {
	t.mu.Lock()
	if t.gate != nil && time.Since(t.gateAt) < t.ttl {
		g := t.gate
		t.mu.Unlock()
		return g, nil
	}
	t.mu.Unlock()
	var g copyGate
	if err := t.get(ctx, t.marketURL+"/internal/tradfi/gate", &g); err != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.gate != nil {
			return t.gate, nil
		}
		return nil, err
	}
	t.mu.Lock()
	t.gate, t.gateAt = &g, time.Now()
	t.mu.Unlock()
	return &g, nil
}

func (t *copyTradFi) country(ctx context.Context, uid string) (string, error) {
	t.mu.Lock()
	if e, ok := t.kyc[uid]; ok && time.Since(e.at) < t.ttl {
		t.mu.Unlock()
		return e.country, nil
	}
	t.mu.Unlock()
	var out struct {
		Country string `json:"country"`
	}
	if err := t.get(ctx, t.kycURL+"/internal/kyc/country?userId="+url.QueryEscape(uid), &out); err != nil {
		return "", err
	}
	c := strings.ToUpper(strings.TrimSpace(out.Country))
	t.mu.Lock()
	if len(t.kyc) > 50000 {
		t.kyc = map[string]copyKyc{}
	}
	t.kyc[uid] = copyKyc{c, time.Now()}
	t.mu.Unlock()
	return c, nil
}

// copySeededTradFi: TradFi perps seeded by migration (fallback when no gate ever loaded).
var copySeededTradFi = map[string]bool{"XAGUSDT": true, "WTIUSDT": true}

// blocked returns "" when the follower may open sym, else the skip reason.
func (t *copyTradFi) blocked(ctx context.Context, sym, follower string) string {
	if t == nil {
		return ""
	}
	sym = strings.ToUpper(sym)
	g, err := t.snapshot(ctx)
	if err != nil {
		if copySeededTradFi[sym] {
			return "TRADFI_UNAVAILABLE"
		}
		return ""
	}
	flag := g.FuturesSymbols[sym]
	if flag == "" {
		return ""
	}
	if !g.Features[flag] {
		return "FEATURE_DISABLED"
	}
	c, err := t.country(ctx, follower)
	if err != nil {
		return "TRADFI_UNAVAILABLE"
	}
	for _, r := range g.RestrictedCountries {
		if c != "" && strings.ToUpper(r) == c {
			return "REGION_RESTRICTED"
		}
	}
	return ""
}
