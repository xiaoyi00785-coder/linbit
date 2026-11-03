package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pquerna/otp/totp"
)

// newTestServer spins up a Server against the DATABASE_URL database, or skips.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Skipf("DATABASE_URL set but unreachable (%v); skipping", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := Config{
		JWTSecret:        "test-secret",
		AccessTTL:        time.Hour,
		RefreshTTL:       24 * time.Hour,
		ResetTokenTTL:    10 * time.Minute,
		DevMode:          true,
		InternalToken:    "test-internal",
		StorageBackend:   "local",
		AvatarDir:        t.TempDir(),
		AvatarPublicBase: "/account/avatars",
		AvatarMaxBytes:   2 * 1024 * 1024,
	}
	return NewServer(cfg, NewStore(db), nil)
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONDevice(t, h, method, path, token, "", body)
}

// doJSONDevice is doJSON with an explicit User-Agent (to simulate devices).
func doJSONDevice(t *testing.T, h http.Handler, method, path, token, ua string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, strings.NewReader(string(b)))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Content-Type", "application/json")
	if ua != "" {
		r.Header.Set("User-Agent", ua)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var seq int64

func uniqueEmail() string {
	return fmt.Sprintf("trader+%d-%d@example.com", time.Now().UnixNano(), atomic.AddInt64(&seq, 1))
}

func uniquePhone() string {
	n := (time.Now().UnixNano() + atomic.AddInt64(&seq, 1)) % 1_000_000_000
	return fmt.Sprintf("+8613%09d", n)
}

// registerWithCode runs send-code -> register and returns the issued tokens.
func registerWithCode(t *testing.T, srv *Server, email, phone, cc string) authTokens {
	t.Helper()
	h := srv.Routes()
	send := map[string]string{"purpose": purposeRegister}
	var target string
	if email != "" {
		send["email"] = email
		target = strings.ToLower(email)
	} else {
		send["phone"], send["countryCode"] = phone, cc
		norm, _, _ := normalizePhone(phone, cc)
		target = norm
	}
	if w := doJSON(t, h, "POST", "/auth/code/send", "", send); w.Code != http.StatusOK {
		t.Fatalf("send register code: %d %s", w.Code, w.Body.String())
	}
	code := srv.recorder.Last(target, purposeRegister)
	if code == "" {
		t.Fatalf("no register code recorded for %s", target)
	}
	reg := map[string]string{"password": "Str0ng-Passw0rd!", "code": code}
	if email != "" {
		reg["email"] = email
	} else {
		reg["phone"], reg["countryCode"] = phone, cc
	}
	w := doJSON(t, h, "POST", "/auth/register", "", reg)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	var tok authTokens
	if err := json.Unmarshal(w.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode tokens: %v", err)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatal("empty tokens from register")
	}
	return tok
}

func TestEmailRegisterDuplicateAndRefresh(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()

	tok := registerWithCode(t, srv, email, "", "")

	// Duplicate registration needs a fresh code, then 409.
	_ = doJSON(t, h, "POST", "/auth/code/send", "", map[string]string{"purpose": purposeRegister, "email": email})
	code := srv.recorder.Last(strings.ToLower(email), purposeRegister)
	w := doJSON(t, h, "POST", "/auth/register", "", map[string]string{"email": email, "password": "Str0ng-Passw0rd!", "code": code})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate register = %d, want 409", w.Code)
	}

	// Account endpoint requires auth.
	if w := doJSON(t, h, "GET", "/account", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth account = %d", w.Code)
	}
	w = doJSON(t, h, "GET", "/account", tok.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account = %d %s", w.Code, w.Body.String())
	}
	var acct accountResponse
	_ = json.Unmarshal(w.Body.Bytes(), &acct)
	if acct.Email == nil || *acct.Email != strings.ToLower(email) || !acct.EmailVerified {
		t.Fatalf("unexpected account %+v", acct)
	}

	// Refresh rotation: old token cannot be replayed.
	if w := doJSON(t, h, "POST", "/auth/refresh", "", refreshRequest{RefreshToken: tok.RefreshToken}); w.Code != http.StatusOK {
		t.Fatalf("refresh = %d", w.Code)
	}
	if w := doJSON(t, h, "POST", "/auth/refresh", "", refreshRequest{RefreshToken: tok.RefreshToken}); w.Code != http.StatusUnauthorized {
		t.Fatalf("replayed refresh = %d, want 401", w.Code)
	}
}

func TestRegisterRejectsBadCode(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	_ = doJSON(t, h, "POST", "/auth/code/send", "", map[string]string{"purpose": purposeRegister, "email": email})
	w := doJSON(t, h, "POST", "/auth/register", "", map[string]string{"email": email, "password": "Str0ng-Passw0rd!", "code": "000000"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("register with wrong code = %d, want 400", w.Code)
	}
	// Missing code entirely.
	w = doJSON(t, h, "POST", "/auth/register", "", map[string]string{"email": uniqueEmail(), "password": "Str0ng-Passw0rd!"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("register without code = %d, want 400", w.Code)
	}
}

func TestPhoneRegisterAndTwoStepLoginNewDevice(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	phone := uniquePhone()

	registerWithCode(t, srv, "", phone, "86")

	// Login from a NEW device -> challenge (SMS), not tokens.
	w := doJSONDevice(t, h, "POST", "/auth/login", "", "device-A",
		map[string]string{"phone": phone, "password": "Str0ng-Passw0rd!", "deviceId": "device-A", "deviceName": "iPhone"})
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d %s", w.Code, w.Body.String())
	}
	var ch loginChallengeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &ch)
	if ch.ChallengeToken == "" || len(ch.Methods) != 1 || ch.Methods[0] != channelSMS {
		t.Fatalf("expected sms challenge, got %+v (body=%s)", ch, w.Body.String())
	}

	// Wrong code -> 400.
	if w := doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "code": "000000"}); w.Code != http.StatusBadRequest {
		t.Fatalf("verify wrong code = %d, want 400", w.Code)
	}

	// Resend + correct code -> tokens.
	if w := doJSON(t, h, "POST", "/auth/login/code", "", map[string]string{"challengeToken": ch.ChallengeToken}); w.Code != http.StatusOK {
		t.Fatalf("resend = %d %s", w.Code, w.Body.String())
	}
	code := srv.recorder.Last(phone, purposeLogin)
	if code == "" {
		t.Fatal("no login code recorded")
	}
	w = doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "code": code})
	if w.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", w.Code, w.Body.String())
	}
	var tok authTokens
	_ = json.Unmarshal(w.Body.Bytes(), &tok)
	if tok.AccessToken == "" {
		t.Fatal("no tokens after verify")
	}

	// Same device again -> known device, direct tokens (no challenge).
	w = doJSONDevice(t, h, "POST", "/auth/login", "", "device-A",
		map[string]string{"phone": phone, "password": "Str0ng-Passw0rd!", "deviceId": "device-A"})
	var tok2 authTokens
	_ = json.Unmarshal(w.Body.Bytes(), &tok2)
	if w.Code != http.StatusOK || tok2.AccessToken == "" {
		t.Fatalf("known-device login = %d body=%s", w.Code, w.Body.String())
	}

	// Device management: list shows the register device + device-A; remove one.
	w = doJSON(t, h, "GET", "/account/devices", tok.AccessToken, nil)
	var dl devicesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &dl)
	if len(dl.Items) < 2 {
		t.Fatalf("expected >=2 devices, got %d", len(dl.Items))
	}
	if w := doJSON(t, h, "DELETE", "/account/devices/cid:device-A", tok.AccessToken, nil); w.Code != http.StatusNoContent {
		t.Fatalf("remove device = %d %s", w.Code, w.Body.String())
	}
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	// Reset password: step 1 verify-code -> single-use resetToken, step 2 reset.
	_ = doJSON(t, h, "POST", "/auth/code/send", "", map[string]string{"purpose": purposeReset, "email": email})
	code := srv.recorder.Last(strings.ToLower(email), purposeReset)
	if code == "" {
		t.Fatal("no reset code recorded")
	}
	// Wrong code must not mint a token.
	if w := doJSON(t, h, "POST", "/auth/password/verify-code", "", map[string]string{"email": email, "code": "000000"}); w.Code != http.StatusBadRequest {
		t.Fatalf("verify-code wrong = %d, want 400", w.Code)
	}
	// Need a fresh code (the bad attempt may have incremented attempts).
	_ = doJSON(t, h, "POST", "/auth/code/send", "", map[string]string{"purpose": purposeReset, "email": email})
	code = srv.recorder.Last(strings.ToLower(email), purposeReset)
	vw := doJSON(t, h, "POST", "/auth/password/verify-code", "", map[string]string{"email": email, "code": code})
	if vw.Code != http.StatusOK {
		t.Fatalf("verify-code = %d %s", vw.Code, vw.Body.String())
	}
	var vr verifyResetCodeResponse
	_ = json.Unmarshal(vw.Body.Bytes(), &vr)
	if vr.ResetToken == "" {
		t.Fatal("verify-code returned no resetToken")
	}
	w := doJSON(t, h, "POST", "/auth/password/reset", "", map[string]string{"resetToken": vr.ResetToken, "newPassword": "N3w-Passw0rd!"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", w.Code, w.Body.String())
	}
	// resetToken is single-use: a replay is rejected.
	if w := doJSON(t, h, "POST", "/auth/password/reset", "", map[string]string{"resetToken": vr.ResetToken, "newPassword": "An0ther-Pass!"}); w.Code != http.StatusBadRequest {
		t.Fatalf("resetToken replay = %d, want 400", w.Code)
	}

	// Old refresh token revoked.
	if w := doJSON(t, h, "POST", "/auth/refresh", "", refreshRequest{RefreshToken: tok.RefreshToken}); w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after reset = %d, want 401", w.Code)
	}
	// Old password rejected, new password accepted (register device is trusted).
	if w := doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": "Str0ng-Passw0rd!"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("login old pw = %d, want 401", w.Code)
	}
	if w := doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": "N3w-Passw0rd!"}); w.Code != http.StatusOK {
		t.Fatalf("login new pw = %d %s", w.Code, w.Body.String())
	}
}

func TestProfileAndAddressBook(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	target := strings.ToLower(email)

	// Profile update.
	nick, avatar := "lb_Trader", "https://cdn.example.com/a.png"
	w := doJSON(t, h, "PATCH", "/account/profile", tok.AccessToken, map[string]*string{"nickname": &nick, "avatarUrl": &avatar})
	if w.Code != http.StatusOK {
		t.Fatalf("profile = %d %s", w.Code, w.Body.String())
	}
	var acct accountResponse
	_ = json.Unmarshal(w.Body.Bytes(), &acct)
	if acct.Nickname == nil || *acct.Nickname != nick || acct.AvatarURL == nil || *acct.AvatarURL != avatar {
		t.Fatalf("profile not updated: %+v", acct)
	}
	// Invalid avatar rejected.
	bad := "javascript:alert(1)"
	if w := doJSON(t, h, "PATCH", "/account/profile", tok.AccessToken, map[string]*string{"avatarUrl": &bad}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad avatar = %d, want 400", w.Code)
	}

	// Address book add requires a code (no TOTP) -> first without code = 400.
	addr := map[string]any{"asset": "USDT", "network": "TRC20", "address": "TDemoAddr0001", "label": "cold"}
	if w := doJSON(t, h, "POST", "/account/withdraw-addresses", tok.AccessToken, addr); w.Code != http.StatusBadRequest {
		t.Fatalf("add address without code = %d, want 400", w.Code)
	}
	// Get a code, then add succeeds.
	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	addr["code"] = srv.recorder.Last(target, purposeAddress)
	w = doJSON(t, h, "POST", "/account/withdraw-addresses", tok.AccessToken, addr)
	if w.Code != http.StatusCreated {
		t.Fatalf("add address = %d %s", w.Code, w.Body.String())
	}
	var created WithdrawAddress
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("address id empty")
	}

	// List shows it.
	w = doJSON(t, h, "GET", "/account/withdraw-addresses", tok.AccessToken, nil)
	var list addressesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("address list len = %d, want 1", len(list.Items))
	}

	// Enable whitelist (needs a fresh code).
	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	wlCode := srv.recorder.Last(target, purposeAddress)
	w = doJSON(t, h, "PUT", "/account/withdraw-whitelist", tok.AccessToken, map[string]any{"enabled": true, "code": wlCode})
	if w.Code != http.StatusOK {
		t.Fatalf("enable whitelist = %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, h, "GET", "/account/withdraw-whitelist", tok.AccessToken, nil)
	var wl whitelistResponse
	_ = json.Unmarshal(w.Body.Bytes(), &wl)
	if !wl.Enabled {
		t.Fatal("whitelist not enabled")
	}

	// Delete the address (code via query param).
	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	delCode := srv.recorder.Last(target, purposeAddress)
	if w := doJSON(t, h, "DELETE", "/account/withdraw-addresses/"+created.ID+"?code="+delCode, tok.AccessToken, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete address = %d %s", w.Code, w.Body.String())
	}
}

func TestTwoStepLoginTOTP(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	// Bind + enable TOTP.
	w := doJSON(t, h, "POST", "/2fa/totp/bind", tok.AccessToken, nil)
	var bind struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &bind)
	code, _ := totp.GenerateCode(bind.Secret, time.Now())
	if w := doJSON(t, h, "POST", "/2fa/totp/verify", tok.AccessToken, codeRequest{Code: code}); w.Code != http.StatusOK {
		t.Fatalf("totp verify = %d %s", w.Code, w.Body.String())
	}

	// Login now returns a TOTP challenge (even from the registered device).
	w = doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": "Str0ng-Passw0rd!"})
	var ch loginChallengeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &ch)
	if ch.ChallengeToken == "" || ch.Methods[0] != "totp" {
		t.Fatalf("expected totp challenge, got %+v", ch)
	}
	// Complete with a TOTP code.
	code, _ = totp.GenerateCode(bind.Secret, time.Now())
	w = doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "totpCode": code})
	var out authTokens
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || out.AccessToken == "" {
		t.Fatalf("totp login verify = %d %s", w.Code, w.Body.String())
	}
}

func TestCodeRateLimitDBFallback(t *testing.T) {
	srv := newTestServer(t)
	// Exercise the real throttle (DevMode relaxes it): force a 60s interval.
	srv.codes.minInterval = time.Minute
	srv.codes.hourlyMax = 5
	email := uniqueEmail()
	ctx := t.Context()
	if err := srv.codes.Send(ctx, channelEmail, strings.ToLower(email), purposeRegister); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := srv.codes.Send(ctx, channelEmail, strings.ToLower(email), purposeRegister); err != ErrRateLimited {
		t.Fatalf("second send = %v, want ErrRateLimited", err)
	}
}
