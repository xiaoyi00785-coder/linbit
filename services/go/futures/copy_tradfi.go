package main

// 跟单镜像开仓不经过网关，所以这里自己查 TradFi 门禁（docs/tradfi.md）。
// 开关关闭 → FEATURE_DISABLED；跟随者 KYC 国家受限 → REGION_RESTRICTED；门禁或 KYC 查询失败 → TRADFI_UNAVAILABLE（失败即拒绝）。
// 跳过原因写入 fut_copy_trades。减仓和平仓不设门禁。标的如 XAG、WTI。

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

// copySeededTradFi：迁移种下的 TradFi 永续，门禁从未加载时的兜底列表。
var copySeededTradFi = map[string]bool{"XAGUSDT": true, "WTIUSDT": true}

// blocked：跟随者可以开该合约时返回空串，否则返回跳过原因。
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
