package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// TestLegacyV3Aliases: every /user/private/v3/* path the iOS/Flutter shell
// still calls must hit its canonical /account/* handler on the account service
// with the usual auth enforcement.
func TestLegacyV3Aliases(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)

	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())

	cases := []struct {
		method, legacy, canonical string
	}{
		{"POST", "/user/private/v3/account-delete", "/account/delete-request"},
		{"POST", "/user/private/v3/account-delete-cancel", "/account/delete-cancel"},
		{"GET", "/user/private/v3/account-delete-status", "/account/delete-status"},
		{"POST", "/user/private/v3/bind-tg", "/account/bind-telegram"},
		{"POST", "/user/private/v3/unbind-tg", "/account/unbind-telegram"},
		{"GET", "/user/private/v3/tg-info", "/account/telegram"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.legacy, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s = %d, want 200", c.method, c.legacy, w.Code)
		}
		if account.lastPath != c.canonical {
			t.Fatalf("%s %s proxied to %q, want %q", c.method, c.legacy, account.lastPath, c.canonical)
		}
		if account.lastUserID != "80000001" {
			t.Fatalf("%s %s X-User-Id = %q, want 80000001", c.method, c.legacy, account.lastUserID)
		}
	}
}

// TestLegacyV3AliasesRequireAuth: a rewritten alias is still a private route.
func TestLegacyV3AliasesRequireAuth(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)

	for _, p := range []string{
		"/user/private/v3/account-delete",
		"/user/private/v3/bind-tg",
		"/user/private/v3/tg-info",
	} {
		r := httptest.NewRequest("POST", p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without token = %d, want 401", p, w.Code)
		}
	}
	if account.hits != 0 {
		t.Fatalf("legacy alias reached upstream without auth: %d hits", account.hits)
	}
}

// TestLegacyV3Unknown: an unrelated /user/private/v3/* path is NOT rewritten;
// it falls through to the regular 404, not to /account.
func TestLegacyV3Unknown(t *testing.T) {
	account := newUpstream()
	defer account.server.Close()
	h := newTestGateway(t, account.server.URL, account.server.URL)

	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	r := httptest.NewRequest("POST", "/user/private/v3/not-a-real-route", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown legacy path = %d, want 404", w.Code)
	}
	if account.hits != 0 {
		t.Fatal("unknown legacy path leaked to upstream")
	}
}

func TestLegacyAliasFunction(t *testing.T) {
	if p, ok := legacyAlias("/user/private/v3/account-delete"); !ok || p != "/account/delete-request" {
		t.Fatalf("account-delete alias = (%q, %v)", p, ok)
	}
	if _, ok := legacyAlias("/account/delete-request"); ok {
		t.Fatal("canonical path must not be aliased again")
	}
	if _, ok := legacyAlias("/user/private/v3/"); ok {
		t.Fatal("prefix itself must not alias")
	}
	if _, ok := legacyAlias(""); ok {
		t.Fatal("empty path must not alias")
	}
}
