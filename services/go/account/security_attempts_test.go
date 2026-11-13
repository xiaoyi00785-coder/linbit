package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// A2–A5 regression tests. DB-backed: newTestServer skips without DATABASE_URL.

// totpAt returns the authenticator code for secret at t.
func totpAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatalf("generate totp: %v", err)
	}
	return code
}

// loginChallenge starts a password login and decodes the challenge body.
func loginChallenge(t *testing.T, h http.Handler, email, password string) (*httptest.ResponseRecorder, loginChallengeResponse) {
	t.Helper()
	w := doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": password})
	var ch loginChallengeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &ch)
	return w, ch
}

func TestTOTPRejectsReusedCode(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	secret := enableTOTP(t, srv, tok.AccessToken, email)

	now := srv.now()
	code := totpAt(t, secret, now)

	// First use of the code completes a login.
	w, ch := loginChallenge(t, h, email, "Str0ng-Passw0rd!")
	if w.Code != http.StatusOK || ch.ChallengeToken == "" {
		t.Fatalf("login challenge = %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "totpCode": code}); w.Code != http.StatusOK {
		t.Fatalf("first verify = %d %s", w.Code, w.Body.String())
	}
	// The same code must not work again, even for a fresh challenge.
	_, ch2 := loginChallenge(t, h, email, "Str0ng-Passw0rd!")
	w = doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch2.ChallengeToken, "totpCode": code})
	if w.Code != http.StatusBadRequest || errCode(t, w.Body.Bytes()) != "TOTP_REPLAYED" {
		t.Fatalf("replayed verify = %d %s, want 400 TOTP_REPLAYED", w.Code, w.Body.String())
	}
}

func TestTOTPLocksAfterRepeatedFailures(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	secret := enableTOTP(t, srv, tok.AccessToken, email)

	// Five wrong codes in a row lock TOTP; each answers 401 like any wrong code.
	_, ch := loginChallenge(t, h, email, "Str0ng-Passw0rd!")
	bad := "000000"
	for _, at := range []time.Time{srv.now().Add(-totpPeriod * time.Second), srv.now(), srv.now().Add(totpPeriod * time.Second)} {
		if totpAt(t, secret, at) == bad {
			bad = "999999" // the real code happens to be 000000 in the skew window
		}
	}
	for i := 0; i < totpMaxFailures; i++ {
		w := doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "totpCode": bad})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s, want 401", i+1, w.Code, w.Body.String())
		}
	}
	// While locked, even a correct code is refused without being evaluated.
	_, ch2 := loginChallenge(t, h, email, "Str0ng-Passw0rd!")
	w := doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch2.ChallengeToken, "totpCode": totpAt(t, secret, srv.now())})
	if w.Code != http.StatusTooManyRequests || errCode(t, w.Body.Bytes()) != "TOTP_LOCKED" {
		t.Fatalf("verify while locked = %d %s, want 429 TOTP_LOCKED", w.Code, w.Body.String())
	}
	// A correct code on a sensitive action is refused the same way.
	w = doJSON(t, h, "POST", "/2fa/totp/disable", tok.AccessToken, codeRequest{Code: totpAt(t, secret, srv.now())})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("disable while locked = %d %s, want 429", w.Code, w.Body.String())
	}
}

func TestPasswordLoginLocksAfterRepeatedFailures(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	registerWithCode(t, srv, email, "", "")

	// The attempt that reaches the limit still answers 401 (it was a wrong
	// password); it is the next attempt that is refused outright.
	for i := 0; i < loginMaxFailures; i++ {
		w := doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": "Wr0ng-Passw0rd!"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s, want 401", i+1, w.Code, w.Body.String())
		}
	}
	// Even the right password is refused while locked.
	w := doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": "Str0ng-Passw0rd!"})
	if w.Code != http.StatusTooManyRequests || errCode(t, w.Body.Bytes()) != "AUTH_LOCKED" {
		t.Fatalf("login while locked = %d %s, want 429 AUTH_LOCKED", w.Code, w.Body.String())
	}
	// A wrong password on an unregistered account still answers like a real one.
	if w := doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": uniqueEmail(), "password": "Wr0ng-Passw0rd!"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown account = %d, want 401", w.Code)
	}
}

func TestUnknownAccountLoginCostsBcrypt(t *testing.T) {
	// Compile-time-ish guard that the dummy hash exists and is a valid bcrypt
	// hash: an empty string would make CompareHashAndPassword return instantly
	// and reintroduce the timing difference.
	if dummyPasswordHash == "" || !checkPassword(dummyPasswordHash, "linbit-timing-equalizer") {
		t.Fatal("dummyPasswordHash is not a usable bcrypt hash")
	}
	if checkPassword(dummyPasswordHash, "some-other-password") {
		t.Fatal("dummy hash accepted an unrelated password")
	}
}
