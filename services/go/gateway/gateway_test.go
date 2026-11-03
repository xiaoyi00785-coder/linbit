package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func newTestGatewayCfg(t *testing.T, cfg Config) http.Handler {
	t.Helper()
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "test-secret"
	}
	gw, err := NewGateway(cfg)
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

func TestWalletRouteProxiesToAsset(t *testing.T) {
	asset := newUpstream()
	defer asset.server.Close()
	h := newTestGateway(t, asset.server.URL, asset.server.URL)

	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	r := httptest.NewRequest("GET", "/wallet/withdraw/networks?asset=USDT", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("wallet status = %d, want 200", w.Code)
	}
	if asset.lastPath != "/wallet/withdraw/networks" {
		t.Fatalf("proxied path = %q", asset.lastPath)
	}
	if asset.lastUserID != "80000001" {
		t.Fatalf("injected user id = %q", asset.lastUserID)
	}
}

func TestWalletRouteRequiresToken(t *testing.T) {
	asset := newUpstream()
	defer asset.server.Close()
	h := newTestGateway(t, asset.server.URL, asset.server.URL)

	r := httptest.NewRequest("GET", "/wallet/withdraw/records", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wallet no-token status = %d, want 401", w.Code)
	}
	if asset.hits != 0 {
		t.Fatal("wallet request without token reached upstream")
	}
}

func TestCORSPreflightDevDefault(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)

	r := httptest.NewRequest("OPTIONS", "/assets/balances", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Access-Control-Request-Method", "GET")
	r.Header.Set("Access-Control-Request-Headers", "authorization")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("allow-origin = %q", got)
	}
	if h := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(h, "Authorization") || !strings.Contains(h, "Content-Type") {
		t.Fatalf("allow-headers = %q, want Authorization + Content-Type", h)
	}
	if account.hits != 0 {
		t.Fatal("preflight reached upstream")
	}
}

func TestCORSDevDefault127(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)

	r := httptest.NewRequest("OPTIONS", "/assets/balances", nil)
	r.Header.Set("Origin", "http://127.0.0.1:5173")
	r.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5173" {
		t.Fatalf("127.0.0.1 origin not allowed by dev default")
	}
}

func TestCORSDisallowedOriginNoHeader(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	// Explicit allow-list: only the configured origin is permitted.
	h := newTestGatewayCfg(t, Config{
		AccountURL:     account.server.URL,
		AssetURL:       account.server.URL,
		AllowedOrigins: []string{"https://app.linbit.com"},
	})

	r := httptest.NewRequest("OPTIONS", "/assets/balances", nil)
	r.Header.Set("Origin", "http://evil.example.com")
	r.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin got allow-origin = %q", got)
	}

	// The configured origin is allowed.
	r2 := httptest.NewRequest("OPTIONS", "/assets/balances", nil)
	r2.Header.Set("Origin", "https://app.linbit.com")
	r2.Header.Set("Access-Control-Request-Method", "GET")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Header().Get("Access-Control-Allow-Origin") != "https://app.linbit.com" {
		t.Fatalf("configured origin not allowed")
	}
}

func TestCORSActualRequestHasOriginHeader(t *testing.T) {
	asset := newUpstream()
	defer asset.server.Close()
	h := newTestGateway(t, asset.server.URL, asset.server.URL)

	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	r := httptest.NewRequest("GET", "/assets/balances", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatal("actual request missing CORS allow-origin header")
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
