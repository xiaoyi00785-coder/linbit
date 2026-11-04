package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// fakeAccount answers /internal/api-keys/verify and records proxied hits.
func fakeAccount(t *testing.T, perms []string) (*httptest.Server, *int32, *string) {
	var hits int32
	var lastUID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/api-keys/verify" {
			if r.Header.Get("X-Internal-Token") != "itok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			v := keyVerdict{OK: b["apiKey"] == "lbk_good" && b["apiSecret"] == "s3cret", UID: "U_SUB", ParentUID: "U_PARENT", Permissions: perms}
			if !v.OK {
				v.Reason = "invalid"
			} else if b["ip"] == "6.6.6.6" {
				v = keyVerdict{OK: false, Reason: "ip_not_allowed"}
			}
			json.NewEncoder(w).Encode(v)
			return
		}
		atomic.AddInt32(&hits, 1)
		lastUID = r.Header.Get("X-User-Id")
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &lastUID
}

func keyGateway(t *testing.T, up string) http.Handler {
	return newTestGatewayCfg(t, Config{AccountURL: up, AssetURL: up, WalletURL: up, OrderURL: up, FuturesURL: up, EarnURL: up, InternalToken: "itok", ClientIPHeader: "X-Real-IP", TrustedProxyCIDRs: "192.0.2.1/32"}) // httptest peer = nginx
}

func doKey(h http.Handler, method, path, key, ip string) int {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(apiKeyHeader, key)
	req.Header.Set(apiSecretHeader, "s3cret")
	req.Header.Set("X-User-Id", "U_PARENT") // spoof attempt must be ignored
	if ip != "" {
		req.Header.Set("X-Real-IP", ip)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestSubAPIKeyNeverReachesParentOrWithdraw(t *testing.T) {
	up, hits, _ := fakeAccount(t, []string{"read", "spot_trade", "futures_trade"})
	h := keyGateway(t, up.URL)
	for _, c := range []struct{ m, p string }{
		{"POST", "/wallet/withdraw"}, {"GET", "/wallet/withdrawals"}, {"GET", "/wallet/deposit/address"},
		{"POST", "/account/withdraw-addresses"}, {"GET", "/account/withdraw-whitelist"},
		{"GET", "/sub-accounts"}, {"POST", "/sub-accounts"}, {"POST", "/sub-accounts/transfer"},
		{"POST", "/sub-accounts/1/api-keys"}, {"GET", "/account/me"}, {"POST", "/auth/logout"},
		{"POST", "/2fa/totp/disable"}, {"POST", "/earn/subscribe"},
		{"POST", "/spot/../wallet/withdraw"}, {"GET", "/spot//../sub-accounts"},
	} {
		if code := doKey(h, c.m, c.p, "lbk_good", ""); code != http.StatusForbidden && code != http.StatusNotFound {
			t.Errorf("%s %s: got %d, want 403", c.m, c.p, code)
		}
	}
	if *hits != 0 {
		t.Fatalf("denied requests reached upstream %d times", *hits)
	}
}

func TestSubAPIKeyPermissions(t *testing.T) {
	up, hits, uid := fakeAccount(t, []string{"read"})
	h := keyGateway(t, up.URL)
	if code := doKey(h, "POST", "/spot/orders", "lbk_good", ""); code != http.StatusForbidden {
		t.Fatalf("read-only key POST /spot/orders: %d", code)
	}
	if code := doKey(h, "POST", "/futures/orders", "lbk_good", ""); code != http.StatusForbidden {
		t.Fatalf("read-only key POST /futures/orders: %d", code)
	}
	if code := doKey(h, "GET", "/spot/orders/open", "lbk_good", ""); code != http.StatusOK {
		t.Fatalf("read key GET open orders: %d", code)
	}
	if *uid != "U_SUB" || *hits != 1 {
		t.Fatalf("upstream saw uid=%q hits=%d; want sub uid, spoofed header stripped", *uid, *hits)
	}
	if code := doKey(h, "GET", "/assets/balances", "lbk_bad", ""); code != http.StatusUnauthorized {
		t.Fatalf("bad key: %d", code)
	}
	if code := doKey(h, "GET", "/assets/balances", "lbk_good", "6.6.6.6"); code != http.StatusForbidden {
		t.Fatalf("ip not allowed: %d", code)
	}

	up2, _, _ := fakeAccount(t, []string{"read", "spot_trade"})
	h2 := keyGateway(t, up2.URL)
	if code := doKey(h2, "POST", "/spot/orders", "lbk_good", ""); code != http.StatusOK {
		t.Fatalf("spot_trade key POST /spot/orders: %d", code)
	}
	if code := doKey(h2, "POST", "/futures/orders", "lbk_good", ""); code != http.StatusForbidden {
		t.Fatalf("spot-only key POST /futures/orders: %d", code)
	}
}

func TestSubSessionFenced(t *testing.T) {
	up, hits, _ := fakeAccount(t, nil)
	h := keyGateway(t, up.URL)
	now := time.Now()
	tok, err := common.SignAccessTokenFor("test-secret", "U_SUB", "x@sub.linbit.invalid", "U_PARENT", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	do := func(m, p string) int {
		req := httptest.NewRequest(m, p, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, c := range []struct{ m, p string }{
		{"POST", "/wallet/withdraw"}, {"GET", "/wallet/deposit/address"}, {"POST", "/account/withdraw-addresses"},
		{"GET", "/sub-accounts"}, {"POST", "/sub-accounts"}, {"POST", "/sub-accounts/5/freeze"},
		{"POST", "/sub-accounts/5/api-keys"}, {"POST", "/2fa/totp/disable"},
		{"POST", "/wallet/internal-transfer"}, {"GET", "/wallet/internal-transfers"},
		{"POST", "/account/password/change"}, {"PUT", "/account/email"}, {"PUT", "/account/phone"},
		{"POST", "/account/contact/code"}, {"POST", "/account/code/send"},
		{"POST", "/account/fund-password"}, {"PUT", "/account/fund-password"}, {"POST", "/account/fund-password/reset"},
		{"PUT", "/account/anti-phishing"}, {"POST", "/account/passkeys/register/begin"}, {"DELETE", "/account/passkeys/3"},
		{"POST", "/2fa/totp/bind"}, {"POST", "/chat/rooms/BTCUSDT/messages"}, {"POST", "/chat/messages/9/report"},
	} {
		if code := do(c.m, c.p); code != http.StatusForbidden {
			t.Errorf("sub JWT %s %s: %d want 403", c.m, c.p, code)
		}
	}
	if *hits != 0 {
		t.Fatalf("fenced requests reached upstream")
	}
	if code := do("POST", "/sub-accounts/transfer"); code != http.StatusOK {
		t.Fatalf("sub transfer back to parent: %d", code)
	}
	for _, c := range []struct{ m, p string }{
		{"GET", "/assets/balances"}, {"GET", "/account/security"}, {"GET", "/account/passkeys"},
		{"POST", "/event/orders"}, {"POST", "/spot/orders"},
	} {
		if code := do(c.m, c.p); code != http.StatusOK {
			t.Fatalf("sub allowed %s %s: %d", c.m, c.p, code)
		}
	}
	// Parent (normal) token still reaches /sub-accounts.
	ptok, _ := common.SignAccessToken("test-secret", "U_PARENT", "p@x.com", time.Hour, now)
	req := httptest.NewRequest("GET", "/sub-accounts", nil)
	req.Header.Set("Authorization", "Bearer "+ptok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("parent /sub-accounts: %d", rec.Code)
	}
}

func TestAPIKeyAllowedTable(t *testing.T) {
	all := []string{"read", "spot_trade", "futures_trade"}
	for _, c := range []struct{ m, p string }{
		{"POST", "/wallet/internal-transfer"}, {"POST", "/account/fund-password"}, {"PUT", "/account/anti-phishing"},
		{"POST", "/account/passkeys/register/begin"}, {"POST", "/event/orders"}, {"POST", "/chat/rooms/BTCUSDT/messages"},
		{"POST", "/assets/transfer"}, {"POST", "/convert/accept"},
	} {
		if apiKeyAllowed(all, c.m, c.p) {
			t.Errorf("api key allowed %s %s", c.m, c.p)
		}
	}
	if apiKeyAllowed([]string{"read", "spot_trade", "futures_trade"}, "POST", "/wallet/withdraw") {
		t.Fatal("withdraw allowed")
	}
	if !apiKeyAllowed([]string{"futures_trade"}, "POST", "/futures/orders") {
		t.Fatal("futures trade denied")
	}
	if apiKeyAllowed([]string{"read"}, "DELETE", "/spot/orders/1") {
		t.Fatal("read key may cancel")
	}
}
