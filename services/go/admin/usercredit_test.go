package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserCreditRefusedOnMainnet(t *testing.T) {
	db, _ := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1")
	defer db.Close()
	s := NewServer(Config{MMUserID: "mm:main"}, db)
	body := `{"asset":"USDT","amount":"10000","ref":"r1","reason":"qa","totpCode":"000000"}`
	for _, env := range []map[string]string{
		{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "APP_ENV": "mainnet"},
		{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": "true", "LINBIT_MAINNET": "true"},
		{"WALLET_CHAIN": "mainnet", "ADMIN_USER_CREDIT_ENABLED": "true"},
		{"WALLET_CHAIN": "testnet", "ADMIN_USER_CREDIT_ENABLED": ""},
	} {
		for _, k := range []string{"WALLET_CHAIN", "ADMIN_USER_CREDIT_ENABLED", "APP_ENV", "NETWORK", "LINBIT_NETWORK", "LINBIT_MAINNET", "MAINNET"} {
			t.Setenv(k, env[k])
		}
		r := httptest.NewRequest(http.MethodPost, "/admin/v1/users/U1/credit", strings.NewReader(body))
		r.SetPathValue("uid", "U1")
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &actor{ID: 1, Username: "t", TOTP: "JBSWY3DPEHPK3PXP"}))
		w := httptest.NewRecorder()
		s.handleUserCredit(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%v: want 404, got %d %s", env, w.Code, w.Body.String())
		}
	}
}

func TestValidateUserCredit(t *testing.T) {
	ok := func(uid, asset, amt, ref string) bool { return validateUserCredit(uid, asset, amt, ref) == "" }
	if !ok("U1", "USDT", "10000", "r") || !ok("U1", "USDC", "0.5", "r") {
		t.Fatal("valid rejected")
	}
	for _, c := range [][4]string{{"", "USDT", "1", "r"}, {"mm:main", "USDT", "1", "r"}, {"system:x", "USDT", "1", "r"},
		{"U1", "BTC", "1", "r"}, {"U1", "USDT", "0", "r"}, {"U1", "USDT", "-1", "r"}, {"U1", "USDT", "1.000000001", "r"}, {"U1", "USDT", "1", ""}} {
		if ok(c[0], c[1], c[2], c[3]) {
			t.Fatalf("invalid accepted: %v", c)
		}
	}
}
