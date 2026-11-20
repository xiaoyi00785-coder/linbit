package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

func TestHandleTokenRequiresAuth(t *testing.T) {
	s := &Server{cfg: Config{JWTSecret: "s", IMTokenTTL: time.Hour}, now: time.Now}
	w := httptest.NewRecorder()
	s.handleToken(w, httptest.NewRequest(http.MethodPost, "/im/token", strings.NewReader(`{}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without auth, got %d", w.Code)
	}
}

func TestHandleTokenIssuesSignedJWT(t *testing.T) {
	cfg := Config{JWTSecret: "test-secret", IMTokenTTL: time.Hour}
	s := &Server{cfg: cfg, now: time.Now}
	req := httptest.NewRequest(http.MethodPost, "/im/token", strings.NewReader(`{}`))
	req.Header.Set("X-User-Id", "u-42")
	w := httptest.NewRecorder()
	s.handleToken(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"userId":"u-42"`) || !strings.Contains(body, `"usersig"`) {
		t.Fatalf("missing fields in %s", body)
	}
	// The returned usersig must be a Linbit access token for u-42.
	start := strings.Index(body, `"usersig":"`) + len(`"usersig":"`)
	end := strings.Index(body[start:], `"`) + start
	tok := body[start:end]
	claims, err := common.ParseAccessToken(cfg.JWTSecret, tok)
	if err != nil || claims.Subject != "u-42" {
		t.Fatalf("parse usersig: %v claims=%+v", err, claims)
	}
}

func TestShortUID(t *testing.T) {
	if shortUID("abc") != "abc" {
		t.Fatal("short pass-through")
	}
	if shortUID("abcdefghij") != "abcdefgh" {
		t.Fatal("long trim")
	}
}
