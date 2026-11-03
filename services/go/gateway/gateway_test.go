package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// upstream records the last request it received and echoes the X-User-Id.
type upstream struct {
	server     *httptest.Server
	lastPath   string
	lastUserID string
	hits       int
}

func newUpstream() *upstream {
	u := &upstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits++
		u.lastPath = r.URL.Path
		u.lastUserID = r.Header.Get("X-User-Id")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("upstream-ok"))
	}))
	return u
}

func newTestGateway(t *testing.T, accountURL, assetURL string) http.Handler {
	t.Helper()
	gw, err := NewGateway(Config{JWTSecret: "test-secret", AccountURL: accountURL, AssetURL: assetURL})
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}
	return gw.Handler()
}

func TestPublicRouteNoAuth(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)

	r := httptest.NewRequest("POST", "/auth/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("public login status = %d", w.Code)
	}
	if account.lastPath != "/auth/login" {
		t.Fatalf("proxied path = %q", account.lastPath)
	}
}

func TestProtectedRouteRequiresToken(t *testing.T) {
	asset := newUpstream()
	defer asset.server.Close()
	h := newTestGateway(t, asset.server.URL, asset.server.URL)

	r := httptest.NewRequest("GET", "/assets/balances", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", w.Code)
	}
	if asset.hits != 0 {
		t.Fatal("request without token reached upstream")
	}
}

func TestProtectedRouteInjectsUserID(t *testing.T) {
	asset := newUpstream()
	defer asset.server.Close()
	h := newTestGateway(t, asset.server.URL, asset.server.URL)

	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	r := httptest.NewRequest("GET", "/assets/balances", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-User-Id", "99999999") // spoof attempt
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("authed status = %d", w.Code)
	}
	if asset.lastUserID != "80000001" {
		t.Fatalf("injected user id = %q, want 80000001 (spoof must be stripped)", asset.lastUserID)
	}
}

func TestInternalBlocked(t *testing.T) {
	asset := newUpstream()
	defer asset.server.Close()
	h := newTestGateway(t, asset.server.URL, asset.server.URL)

	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	r := httptest.NewRequest("POST", "/internal/credit", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("internal status = %d, want 404", w.Code)
	}
	if asset.hits != 0 {
		t.Fatal("internal path reached upstream")
	}
}

func TestUnknownRoute(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)
	r := httptest.NewRequest("GET", "/nope", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", w.Code)
	}
}
