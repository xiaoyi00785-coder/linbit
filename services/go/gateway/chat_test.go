package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

func TestChatRoutes(t *testing.T) {
	market, other := newUpstream(), newUpstream()
	defer market.server.Close()
	defer other.server.Close()
	h := newTestGatewayCfg(t, Config{AccountURL: other.server.URL, AssetURL: other.server.URL, MarketURL: market.server.URL})
	for _, p := range []string{"/chat/rooms", "/chat/rooms/spot.BTCUSDT/messages?limit=20"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != http.StatusOK || market.lastUserID != "" {
			t.Fatalf("%s: status %d uid %q", p, w.Code, market.lastUserID)
		}
	}
	for _, p := range []string{"/chat/rooms/general/messages", "/chat/messages/1/report"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", p, strings.NewReader(`{}`)))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous POST %s = %d", p, w.Code)
		}
	}
	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	req := httptest.NewRequest("POST", "/chat/rooms/general/messages", strings.NewReader(`{"clientId":"c1","text":"hi"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-User-Id", "spoofed")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || market.lastUserID != "80000001" || market.lastPath != "/chat/rooms/general/messages" {
		t.Fatalf("authed POST: %d uid=%q path=%s", w.Code, market.lastUserID, market.lastPath)
	}
}
