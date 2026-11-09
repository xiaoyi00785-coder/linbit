package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestDepositPreferenceAndIdempotentCredit(t *testing.T) {
	store, _ := newTestStore(t)
	srv := &Server{store: store}
	uid := testUser()
	h := srv.Routes()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-User-Id", uid)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call(http.MethodPut, "/assets/deposit-account", `{"account":"futures"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "DEPOSIT_ACCOUNT_UNSUPPORTED") {
		t.Fatalf("futures: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPut, "/assets/deposit-account", `{"account":"spot"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"account":"spot"`) || !strings.Contains(w.Body.String(), `"source":"user"`) {
		t.Fatalf("set spot: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPut, "/assets/deposit-account", `{"account":"spot"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"account":"spot"`) {
		t.Fatalf("replay spot: %d %s", w.Code, w.Body.String())
	}
	ctx := t.Context()
	amt := decimal.RequireFromString("12.5")
	ref := "dep:test:" + uid + ":0"
	if err := store.DepositTo(ctx, AcctSpot, uid, "USDT", amt, ref); err != nil {
		t.Fatal(err)
	}
	if err := store.DepositTo(ctx, AcctSpot, uid, "USDT", amt, ref); err != ErrAlreadyApplied {
		t.Fatalf("replay credit: %v", err)
	}
	if err := store.DepositTo(ctx, AcctFunding, uid, "USDT", amt, ref); err != ErrAlreadyApplied {
		t.Fatalf("cross-account replay: %v", err)
	}
	spot, err := store.WalletBalances(ctx, AcctSpot, uid)
	if err != nil {
		t.Fatal(err)
	}
	funding, err := store.WalletBalances(ctx, AcctFunding, uid)
	if err != nil {
		t.Fatal(err)
	}
	if b := findAsset(spot, "USDT"); b == nil || !b.Free.Equal(amt) {
		t.Fatalf("spot %+v", spot)
	}
	if b := findAsset(funding, "USDT"); b != nil && b.Free.Sign() != 0 {
		t.Fatalf("funding credited %+v", b)
	}
}
