package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
		JWTSecret:  "test-secret",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	}
	return NewServer(cfg, NewStore(db))
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, strings.NewReader(string(b)))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func uniqueEmail() string {
	return fmt.Sprintf("trader+%d@example.com", time.Now().UnixNano())
}

func TestRegisterLoginFlow(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()

	// Register.
	w := doJSON(t, h, "POST", "/auth/register", "", registerRequest{Email: email, Password: "Str0ng-Passw0rd!"})
	if w.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body=%s", w.Code, w.Body.String())
	}
	var tokens authTokens
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("decode tokens: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("empty tokens")
	}

	// Duplicate registration -> 409.
	w = doJSON(t, h, "POST", "/auth/register", "", registerRequest{Email: email, Password: "Str0ng-Passw0rd!"})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate register status = %d", w.Code)
	}

	// Login.
	w = doJSON(t, h, "POST", "/auth/login", "", loginRequest{Email: email, Password: "Str0ng-Passw0rd!"})
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", w.Code, w.Body.String())
	}

	// Wrong password -> 401.
	w = doJSON(t, h, "POST", "/auth/login", "", loginRequest{Email: email, Password: "nope"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", w.Code)
	}

	// Account requires auth.
	w = doJSON(t, h, "GET", "/account", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth account status = %d", w.Code)
	}
	w = doJSON(t, h, "GET", "/account", tokens.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account status = %d, body=%s", w.Code, w.Body.String())
	}
	var acct accountResponse
	_ = json.Unmarshal(w.Body.Bytes(), &acct)
	if acct.Email != strings.ToLower(email) || acct.TwoFactorEnabled {
		t.Fatalf("unexpected account: %+v", acct)
	}

	// Refresh rotation: old token cannot be replayed.
	w = doJSON(t, h, "POST", "/auth/refresh", "", refreshRequest{RefreshToken: tokens.RefreshToken})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh status = %d", w.Code)
	}
	w = doJSON(t, h, "POST", "/auth/refresh", "", refreshRequest{RefreshToken: tokens.RefreshToken})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("replayed refresh status = %d, want 401", w.Code)
	}
}

func TestTwoFactorFlow(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()

	w := doJSON(t, h, "POST", "/auth/register", "", registerRequest{Email: email, Password: "Str0ng-Passw0rd!"})
	if w.Code != http.StatusCreated {
		t.Fatalf("register status = %d", w.Code)
	}
	var tokens authTokens
	_ = json.Unmarshal(w.Body.Bytes(), &tokens)

	// Bind TOTP.
	w = doJSON(t, h, "POST", "/2fa/totp/bind", tokens.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("bind status = %d, body=%s", w.Code, w.Body.String())
	}
	var bind struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &bind)

	code, _ := totp.GenerateCode(bind.Secret, time.Now())
	w = doJSON(t, h, "POST", "/2fa/totp/verify", tokens.AccessToken, codeRequest{Code: code})
	if w.Code != http.StatusOK {
		t.Fatalf("verify status = %d, body=%s", w.Code, w.Body.String())
	}

	// Login now requires a valid TOTP code.
	w = doJSON(t, h, "POST", "/auth/login", "", loginRequest{Email: email, Password: "Str0ng-Passw0rd!"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("login without totp status = %d, want 401", w.Code)
	}
	code, _ = totp.GenerateCode(bind.Secret, time.Now())
	w = doJSON(t, h, "POST", "/auth/login", "", loginRequest{Email: email, Password: "Str0ng-Passw0rd!", TOTPCode: code})
	if w.Code != http.StatusOK {
		t.Fatalf("login with totp status = %d, body=%s", w.Code, w.Body.String())
	}

	// Disable 2FA.
	code, _ = totp.GenerateCode(bind.Secret, time.Now())
	w = doJSON(t, h, "POST", "/2fa/totp/disable", tokens.AccessToken, codeRequest{Code: code})
	if w.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body=%s", w.Code, w.Body.String())
	}
}
