package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// A1: a session alone must never be able to take over the account's TOTP —
// enabling needs the email/SMS security code, and an enabled secret can only be
// replaced after /2fa/totp/disable (current TOTP + withdrawal lock).

func errCode(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Code
}

// bindTOTP calls /2fa/totp/bind and returns the minted secret.
func bindTOTP(t *testing.T, h http.Handler, access string) string {
	t.Helper()
	w := doJSON(t, h, "POST", "/2fa/totp/bind", access, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("totp bind = %d %s", w.Code, w.Body.String())
	}
	var b struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if b.Secret == "" {
		t.Fatal("bind returned no secret")
	}
	return b.Secret
}

// securityCode sends a purpose=security code to the user's own contact and
// returns it from the dev recorder.
func securityCode(t *testing.T, srv *Server, h http.Handler, access, target string) string {
	t.Helper()
	if w := doJSON(t, h, "POST", "/account/code/send", access, map[string]string{"purpose": purposeSecurity}); w.Code != http.StatusOK {
		t.Fatalf("send security code = %d %s", w.Code, w.Body.String())
	}
	c := srv.recorder.Last(target, purposeSecurity)
	if c == "" {
		t.Fatalf("no security code recorded for %s", target)
	}
	return c
}

// enableTOTP binds and enables TOTP for an email account, returning the secret.
func enableTOTP(t *testing.T, srv *Server, access, email string) string {
	t.Helper()
	h := srv.Routes()
	secret := bindTOTP(t, h, access)
	code, _ := totp.GenerateCode(secret, time.Now())
	sc := securityCode(t, srv, h, access, strings.ToLower(email))
	if w := doJSON(t, h, "POST", "/2fa/totp/verify", access, totpVerifyRequest{Code: code, SecurityCode: sc}); w.Code != http.StatusOK {
		t.Fatalf("totp verify = %d %s", w.Code, w.Body.String())
	}
	return secret
}

func twoFactorEnabled(t *testing.T, h http.Handler, access string) bool {
	t.Helper()
	w := doJSON(t, h, "GET", "/account", access, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account = %d %s", w.Code, w.Body.String())
	}
	var a accountResponse
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return a.TwoFactorEnabled
}

func TestTOTPEnableRequiresSecurityCode(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	secret := bindTOTP(t, h, tok.AccessToken)
	code, _ := totp.GenerateCode(secret, time.Now())

	// Valid authenticator code but no security code: refused, TOTP stays off.
	w := doJSON(t, h, "POST", "/2fa/totp/verify", tok.AccessToken, totpVerifyRequest{Code: code})
	if w.Code != http.StatusBadRequest || errCode(t, w.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("verify without securityCode = %d %s, want 400 VALIDATION_ERROR", w.Code, w.Body.String())
	}
	// Wrong security code: refused.
	_ = securityCode(t, srv, h, tok.AccessToken, strings.ToLower(email))
	w = doJSON(t, h, "POST", "/2fa/totp/verify", tok.AccessToken, totpVerifyRequest{Code: code, SecurityCode: "000000"})
	if w.Code != http.StatusBadRequest || errCode(t, w.Body.Bytes()) != "CODE_INVALID" {
		t.Fatalf("verify with wrong securityCode = %d %s, want 400 CODE_INVALID", w.Code, w.Body.String())
	}
	if twoFactorEnabled(t, h, tok.AccessToken) {
		t.Fatal("TOTP enabled without a valid security code")
	}

	// A wrong authenticator code is reported as such and does not burn the security code.
	sc := securityCode(t, srv, h, tok.AccessToken, strings.ToLower(email))
	w = doJSON(t, h, "POST", "/2fa/totp/verify", tok.AccessToken, totpVerifyRequest{Code: "000000", SecurityCode: sc})
	if w.Code != http.StatusBadRequest || errCode(t, w.Body.Bytes()) != "TOTP_INVALID" {
		t.Fatalf("verify with wrong totp = %d %s, want 400 TOTP_INVALID", w.Code, w.Body.String())
	}
	code, _ = totp.GenerateCode(secret, time.Now())
	if w := doJSON(t, h, "POST", "/2fa/totp/verify", tok.AccessToken, totpVerifyRequest{Code: code, SecurityCode: sc}); w.Code != http.StatusOK {
		t.Fatalf("verify with both codes = %d %s", w.Code, w.Body.String())
	}
	if !twoFactorEnabled(t, h, tok.AccessToken) {
		t.Fatal("TOTP not enabled after a valid verify")
	}
}

func TestTOTPRebindRejectedWhileEnabled(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	secret := enableTOTP(t, srv, tok.AccessToken, email)

	// With TOTP on, a session cannot mint a new secret or re-run verify.
	w := doJSON(t, h, "POST", "/2fa/totp/bind", tok.AccessToken, nil)
	if w.Code != http.StatusConflict || errCode(t, w.Body.Bytes()) != "TOTP_ALREADY_ENABLED" {
		t.Fatalf("rebind while enabled = %d %s, want 409 TOTP_ALREADY_ENABLED", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "otpauth") {
		t.Fatal("rebind leaked a new otpauth URI")
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	if w := doJSON(t, h, "POST", "/2fa/totp/verify", tok.AccessToken, totpVerifyRequest{Code: code, SecurityCode: "000000"}); w.Code != http.StatusConflict {
		t.Fatalf("verify while enabled = %d %s, want 409", w.Code, w.Body.String())
	}

	// The original secret is untouched: it still completes a TOTP login.
	w = doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": "Str0ng-Passw0rd!"})
	var ch loginChallengeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &ch)
	if ch.ChallengeToken == "" || len(ch.Methods) == 0 || ch.Methods[0] != "totp" {
		t.Fatalf("expected totp challenge, got %d %s", w.Code, w.Body.String())
	}
	code, _ = totp.GenerateCode(secret, time.Now())
	if w := doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "totpCode": code}); w.Code != http.StatusOK {
		t.Fatalf("login with original secret = %d %s", w.Code, w.Body.String())
	}

	// Replacing the authenticator goes through disable (current TOTP) first. A
	// code is single-use, so move the server clock to the next 30s window rather
	// than sleeping, and use that window's code.
	later := time.Now().Add(2 * totpPeriod * time.Second)
	srv.now = func() time.Time { return later }
	code, _ = totp.GenerateCode(secret, later)
	if w := doJSON(t, h, "POST", "/2fa/totp/disable", tok.AccessToken, codeRequest{Code: code}); w.Code != http.StatusOK {
		t.Fatalf("disable = %d %s", w.Code, w.Body.String())
	}
	if fresh := bindTOTP(t, h, tok.AccessToken); fresh == secret {
		t.Fatal("re-bind after disable returned the old secret")
	}
}
