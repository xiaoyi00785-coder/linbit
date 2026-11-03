package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestInternalWhitelist checks the wallet-facing whitelist endpoint: disabled →
// allowed unconditionally; enabled → only whitelisted destinations are allowed.
func TestInternalWhitelist(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("wl%d@example.com", time.Now().UnixNano())
	u, err := s.store.CreateUser(ctx, CreateUserParams{
		Email: &email, PasswordHash: "x", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	get := func(asset, network, address string) bool {
		req := httptest.NewRequest("GET", "/internal/withdraw/whitelist?uid="+u.UID+
			"&asset="+asset+"&network="+network+"&address="+address, nil)
		req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var out internalWhitelistResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.Enabled {
			return out.Allowed
		}
		return out.Allowed // disabled → allowed=true from the handler
	}

	// Whitelist disabled: everything allowed.
	if !get("USDT", "TRC20", "Tanything") {
		t.Fatal("whitelist disabled should allow any address")
	}

	// Enable the whitelist and add one whitelisted address.
	if err := s.store.SetWithdrawWhitelistEnabled(ctx, u.ID, true); err != nil {
		t.Fatalf("enable whitelist: %v", err)
	}
	if _, err := s.store.CreateWithdrawAddress(ctx, u.ID, WithdrawAddress{
		Asset: "USDT", Network: "TRC20", Address: "Tgood", Whitelisted: true,
	}); err != nil {
		t.Fatalf("add address: %v", err)
	}
	if _, err := s.store.CreateWithdrawAddress(ctx, u.ID, WithdrawAddress{
		Asset: "USDT", Network: "TRC20", Address: "Tnotlisted", Whitelisted: false,
	}); err != nil {
		t.Fatalf("add address: %v", err)
	}

	if !get("USDT", "TRC20", "Tgood") {
		t.Fatal("whitelisted address should be allowed")
	}
	if get("USDT", "TRC20", "Tnotlisted") {
		t.Fatal("non-whitelisted address should be blocked")
	}
	if get("USDT", "TRC20", "Tother") {
		t.Fatal("unknown address should be blocked")
	}
}

// TestInternalWhitelistRequiresToken ensures the endpoint is token-gated.
func TestInternalWhitelistRequiresToken(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/internal/withdraw/whitelist?uid=x", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
