package main

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGoogleUnconfigured(t *testing.T) {
	s := &Server{now: time.Now}
	w := httptest.NewRecorder()
	s.handleGoogleLogin(w, httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(`{"idToken":"x"}`)))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleAuthProviders(w, httptest.NewRequest(http.MethodGet, "/auth/providers", nil))
	if !strings.Contains(w.Body.String(), `"clientIds":[]`) {
		t.Fatalf("providers %s", w.Body.String())
	}
}

func TestVerifyGoogleIDToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(aud string, exp time.Time, verified bool) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": "https://accounts.google.com", "aud": aud, "sub": "sub-1",
			"email": "person@example.com", "email_verified": verified,
			"exp": exp.Unix(), "iat": time.Now().Add(-time.Minute).Unix(),
		})
		tok.Header["kid"] = "k1"
		raw, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	keys := map[string]*rsa.PublicKey{"k1": &key.PublicKey}
	now := time.Now()
	id, err := verifyGoogleIDToken(sign("client-1", now.Add(time.Hour), true), keys, []string{"client-1"}, now)
	if err != nil || id.Sub != "sub-1" || id.Email != "person@example.com" {
		t.Fatalf("%+v %v", id, err)
	}
	if _, err := verifyGoogleIDToken(sign("other", now.Add(time.Hour), true), keys, []string{"client-1"}, now); err == nil {
		t.Fatal("wrong audience")
	}
	if _, err := verifyGoogleIDToken(sign("client-1", now.Add(-time.Hour), true), keys, []string{"client-1"}, now); err == nil {
		t.Fatal("expired")
	}
	if _, err := verifyGoogleIDToken(sign("client-1", now.Add(time.Hour), false), keys, []string{"client-1"}, now); err == nil {
		t.Fatal("unverified email")
	}
}

func TestCardAndFiatUnconfigured(t *testing.T) {
	if err := cardDecision("approve", "alice", "alice", "alice", "pending_review", "Issuer", "REF"); err == nil {
		t.Fatal("self approve")
	}
	if err := cardDecision("approve", "cara", "alice", "bob", "pending_review", "", "REF"); err == nil {
		t.Fatal("missing issuer")
	}
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleCardProduct(w, httptest.NewRequest(http.MethodGet, "/cards/product", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"submitted":false`) || !strings.Contains(w.Body.String(), `"issuer":null`) {
		t.Fatalf("card %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleCardApply(w, httptest.NewRequest(http.MethodPost, "/cards/applications", nil))
	// no auth context: unauthorized, and no submission is recorded
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("apply %d %s", w.Code, w.Body.String())
	}
	if configured, reason := s.fiatBuyState(); configured || reason != "unconfigured" {
		t.Fatalf("fiat %v %s", configured, reason)
	}
	s.cfg.FiatOnRampProvider = "example"
	s.cfg.FiatOnRampAPIBase = "https://pay.example.test"
	if configured, reason := s.fiatBuyState(); !configured || reason != "provider_not_integrated" {
		t.Fatalf("fiat configured %v %s", configured, reason)
	}
	w = httptest.NewRecorder()
	s.handleFiatBuyUnavailable(w, httptest.NewRequest(http.MethodPost, "/fiat/buy/quotes", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"charged":false`) || !strings.Contains(w.Body.String(), `"orderCreated":false`) {
		t.Fatalf("quote %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleGuestSupport(w, httptest.NewRequest(http.MethodGet, "/support/guest", nil))
	if !strings.Contains(w.Body.String(), `"mode":"unconfigured"`) || strings.Contains(w.Body.String(), "http") {
		t.Fatalf("guest %s", w.Body.String())
	}
	s.cfg.SupportGuestProvider = "desk"
	s.cfg.SupportGuestEntryURL = "http://insecure.example/chat?token=secret"
	w = httptest.NewRecorder()
	s.handleGuestSupport(w, httptest.NewRequest(http.MethodGet, "/support/guest", nil))
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("leaked %s", w.Body.String())
	}
}
