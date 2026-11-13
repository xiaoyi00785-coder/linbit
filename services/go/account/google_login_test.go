package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// A3: Google sign-in is a first factor only. An account with TOTP must still
// complete the TOTP challenge, and an existing account is never linked to a
// Google identity implicitly.

// googleTestSetup serves a local JWKS and wires the server to trust its tokens.
func googleTestSetup(t *testing.T, srv *Server) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)
	srv.cfg.GoogleJWKSURL = jwks.URL
	srv.cfg.GoogleClientIDs = []string{"test-client"}
	srv.googleKeysCache = nil
	return key, jwks.URL
}

// googleToken signs an RS256 Google ID token for email.
func googleToken(t *testing.T, key *rsa.PrivateKey, email string, now time.Time) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            "test-client",
		"sub":            "google-sub-" + email,
		"email":          email,
		"email_verified": true,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign google token: %v", err)
	}
	return signed
}

func TestGoogleLoginWithTOTPReturnsChallenge(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	secret := enableTOTP(t, srv, tok.AccessToken, email)

	key, _ := googleTestSetup(t, srv)
	// Link the Google identity the way production requires it: from inside the
	// account, which admin/test code does directly via the store.
	me, err := srv.store.UserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if err := srv.store.LinkOAuthIdentity(t.Context(), "google", "google-sub-"+email, me.ID, email); err != nil {
		t.Fatalf("link: %v", err)
	}

	w := doJSON(t, h, "POST", "/auth/google", "", map[string]string{"idToken": googleToken(t, key, email, srv.now())})
	if w.Code != http.StatusOK {
		t.Fatalf("google login = %d %s", w.Code, w.Body.String())
	}
	var ch loginChallengeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &ch)
	if ch.ChallengeToken == "" || len(ch.Methods) != 1 || ch.Methods[0] != "totp" {
		t.Fatalf("google login with TOTP = %s, want a totp challenge and no tokens", w.Body.String())
	}
	var raw map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if _, ok := raw["accessToken"]; ok {
		t.Fatal("google login issued tokens without the second factor")
	}

	// Completing the challenge needs the TOTP code.
	code := totpAt(t, secret, srv.now())
	if w := doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "totpCode": code}); w.Code != http.StatusOK {
		t.Fatalf("complete google challenge = %d %s", w.Code, w.Body.String())
	}
}

func TestGoogleLoginDoesNotAutoLinkExistingAccount(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	registerWithCode(t, srv, email, "", "")
	key, _ := googleTestSetup(t, srv)

	// An account already exists for this address but no Google identity is
	// linked: the login must refuse rather than link silently.
	w := doJSON(t, h, "POST", "/auth/google", "", map[string]string{"idToken": googleToken(t, key, email, srv.now())})
	if w.Code != http.StatusConflict || errCode(t, w.Body.Bytes()) != "GOOGLE_LINK_REQUIRED" {
		t.Fatalf("google login for existing account = %d %s, want 409 GOOGLE_LINK_REQUIRED", w.Code, w.Body.String())
	}
	if _, err := srv.store.OAuthOwner(t.Context(), "google", "google-sub-"+email); err == nil {
		t.Fatal("google identity was linked without the owner's confirmation")
	}
}

func TestGoogleLoginNewAccountCreatesUser(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	key, _ := googleTestSetup(t, srv)
	email := uniqueEmail()

	w := doJSON(t, h, "POST", "/auth/google", "", map[string]string{"idToken": googleToken(t, key, email, srv.now())})
	if w.Code != http.StatusOK {
		t.Fatalf("new google account = %d %s", w.Code, w.Body.String())
	}
	var tok authTokens
	_ = json.Unmarshal(w.Body.Bytes(), &tok)
	if tok.AccessToken == "" {
		t.Fatal("no tokens for a new google account")
	}
}
