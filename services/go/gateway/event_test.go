package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

func TestEventRoutesAndWSAuth(t *testing.T) {
	fut, market, other := newUpstream(), newUpstream(), newUpstream()
	defer fut.server.Close()
	defer market.server.Close()
	defer other.server.Close()
	h := newTestGatewayCfg(t, Config{AccountURL: other.server.URL, AssetURL: other.server.URL, MarketURL: market.server.URL, FuturesURL: fut.server.URL})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/event/products", nil))
	if w.Code != http.StatusOK || fut.lastPath != "/event/products" || fut.lastUserID != "" {
		t.Fatalf("products: %d %s", w.Code, fut.lastPath)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/event/orders", strings.NewReader(`{}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous order = %d", w.Code)
	}
	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	req := httptest.NewRequest("POST", "/event/orders", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || fut.lastUserID != "80000001" {
		t.Fatalf("authed order: %d %q", w.Code, fut.lastUserID)
	}
	// /ws: anonymous stays public; ?token= identifies the user; a spoofed header never does.
	req = httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-User-Id", "spoofed")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || market.lastUserID != "" {
		t.Fatalf("anon ws: %d %q", w.Code, market.lastUserID)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/ws?token="+token, nil))
	if w.Code != http.StatusOK || market.lastUserID != "80000001" {
		t.Fatalf("token ws: %d %q", w.Code, market.lastUserID)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/ws?token=bad", nil))
	if w.Code != http.StatusOK || market.lastUserID != "" {
		t.Fatalf("bad token ws must stay anonymous: %d %q", w.Code, market.lastUserID)
	}
}
