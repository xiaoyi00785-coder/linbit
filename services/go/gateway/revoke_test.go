package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/linbit/platform/services/go/common"
	"github.com/redis/go-redis/v9"
)

func TestLogoutRevokesAccessTokenAtGateway(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	gw, err := NewGateway(Config{JWTSecret: "test-secret", AccountURL: account.server.URL, AssetURL: account.server.URL, WalletURL: account.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	gw.rdb = rdb
	h := gw.Handler()
	tok, _ := common.SignAccessToken("test-secret", "u-1", "a@b.c", time.Hour, time.Now())
	call := func(method, path, token string) int {
		r := httptest.NewRequest(method, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := call("GET", "/account", tok); c != http.StatusOK {
		t.Fatalf("before logout: %d", c)
	}
	_ = common.RevokeAccessTokens(context.Background(), rdb, "u-1", time.Now(), time.Hour)
	if c := call("GET", "/account", tok); c != http.StatusUnauthorized {
		t.Fatalf("after logout: %d, want 401", c)
	}
	// Logout itself is still forwarded (body refreshToken path), without identity.
	hits := account.hits
	if c := call("POST", "/auth/logout", tok); c != http.StatusOK || account.hits != hits+1 || account.lastUserID != "" {
		t.Fatalf("logout passthrough: code=%d uid=%q", c, account.lastUserID)
	}
	if c := call("POST", "/auth/logout", ""); c != http.StatusOK {
		t.Fatalf("logout without token: %d", c)
	}
	if c := call("POST", "/auth/logout", "garbage"); c != http.StatusOK {
		t.Fatalf("logout expired/invalid token: %d", c)
	}
	fresh, _ := common.SignAccessToken("test-secret", "u-1", "a@b.c", time.Hour, time.Now().Add(5*time.Millisecond))
	if c := call("GET", "/account", fresh); c != http.StatusOK {
		t.Fatalf("new login token: %d", c)
	}
}
