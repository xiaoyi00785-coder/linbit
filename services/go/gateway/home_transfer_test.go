package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

func TestHomeRoutesPublicReadOnly(t *testing.T) {
	market, other := newUpstream(), newUpstream()
	defer market.server.Close()
	defer other.server.Close()
	h := newTestGatewayCfg(t, Config{AccountURL: other.server.URL, AssetURL: other.server.URL, MarketURL: market.server.URL})
	for _, p := range []string{"/home/banners?platform=app&locale=zh-CN", "/home/hot?type=gainers"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != http.StatusOK || market.lastUserID != "" {
			t.Fatalf("%s: status %d uid %q", p, w.Code, market.lastUserID)
		}
	}
	if market.hits != 2 || market.lastPath != "/home/hot" {
		t.Fatalf("market hits=%d path=%s", market.hits, market.lastPath)
	}
	// writes and other /home paths need auth (never anonymous)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/home/banners", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST /home/banners = %d", w.Code)
	}
}

func TestAssetsTransferRoutesToFutures(t *testing.T) {
	asset, fut := newUpstream(), newUpstream()
	defer asset.server.Close()
	defer fut.server.Close()
	h := newTestGatewayCfg(t, Config{AccountURL: asset.server.URL, AssetURL: asset.server.URL, FuturesURL: fut.server.URL})
	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	for _, c := range []struct{ method, path string }{{"POST", "/assets/transfer"}, {"GET", "/assets/transfers"}} {
		r := httptest.NewRequest(c.method, c.path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || fut.lastPath != c.path || fut.lastUserID != "80000001" {
			t.Fatalf("%s %s: code=%d path=%s uid=%s", c.method, c.path, w.Code, fut.lastPath, fut.lastUserID)
		}
	}
	r := httptest.NewRequest("GET", "/assets/balances", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if asset.lastPath != "/assets/balances" || fut.hits != 2 {
		t.Fatalf("/assets/balances must stay on asset (asset=%s fut hits=%d)", asset.lastPath, fut.hits)
	}
	// unauthenticated transfer is refused at the gateway
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/assets/transfer", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon transfer = %d", w.Code)
	}
}

func TestHomePlacementsAndNewcomer(t *testing.T) {
	market, growth, other := newUpstream(), newUpstream(), newUpstream()
	defer market.server.Close()
	defer growth.server.Close()
	defer other.server.Close()
	h := newTestGatewayCfg(t, Config{AccountURL: other.server.URL, AssetURL: other.server.URL, MarketURL: market.server.URL, GrowthURL: growth.server.URL})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/home/placements?slots=popup", nil))
	if w.Code != 200 || market.lastPath != "/home/placements" {
		t.Fatalf("placements code=%d path=%s", w.Code, market.lastPath)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/market/fiat-rates?base=USDT", nil))
	if w.Code != 200 || market.lastPath != "/market/fiat-rates" {
		t.Fatalf("fiat-rates code=%d path=%s", w.Code, market.lastPath)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/app/features", nil))
	if w.Code != 200 || market.lastPath != "/app/features" {
		t.Fatalf("app features code=%d path=%s", w.Code, market.lastPath)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/app/features", nil))
	if w.Code != 401 {
		t.Fatalf("anon PUT /app/features = %d", w.Code)
	}
	// anonymous newcomer -> growth without identity
	r := httptest.NewRequest("GET", "/home/newcomer", nil)
	r.Header.Set("X-User-Id", "spoofed")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || growth.lastPath != "/home/newcomer" || growth.lastUserID != "" {
		t.Fatalf("anon newcomer code=%d path=%s uid=%q", w.Code, growth.lastPath, growth.lastUserID)
	}
	// bad token -> still anonymous, not 401
	r = httptest.NewRequest("GET", "/home/newcomer", nil)
	r.Header.Set("Authorization", "Bearer garbage")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || growth.lastUserID != "" {
		t.Fatalf("bad-token newcomer code=%d uid=%q", w.Code, growth.lastUserID)
	}
	token, _ := common.SignAccessToken("test-secret", "80000002", "a@b.c", time.Hour, time.Now())
	r = httptest.NewRequest("GET", "/home/newcomer", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || growth.lastUserID != "80000002" {
		t.Fatalf("authed newcomer code=%d uid=%q", w.Code, growth.lastUserID)
	}
	// optional auth is only for that exact route
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/rewards/newcomer", nil))
	if w.Code != 401 {
		t.Fatalf("/rewards/newcomer anon = %d", w.Code)
	}
}
