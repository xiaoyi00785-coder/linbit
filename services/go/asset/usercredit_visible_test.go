package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

// An admin testnet credit lands in funding_balances and is what both
// GET /assets/overview (funding account) and GET /assets/balances?account=funding
// return — repeatedly (nothing resets it between reads).
func TestUserCreditVisibleInOverviewAndFunding(t *testing.T) {
	creditEnv(t)
	store, _ := newTestStore(t)
	uid := testUser()
	if _, err := store.AdminTestnetCredit(context.Background(), uid, "USDT", decimal.RequireFromString("10000"), "vis-"+uid); err != nil {
		t.Fatal(err)
	}
	h := NewServer(Config{InternalToken: "secret"}, store).Routes()
	do := func(path string, out any) {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-User-Id", uid)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		var ov struct {
			Total    string            `json:"totalValueUsdt"`
			Accounts []overviewAccount `json:"accounts"`
		}
		do("/assets/overview", &ov)
		if ov.Total != "10000.00" {
			t.Fatalf("read %d: overview total = %s, want 10000.00", i, ov.Total)
		}
		var found bool
		for _, a := range ov.Accounts {
			if a.Account != AcctFunding {
				continue
			}
			for _, b := range a.Balances {
				if b.Asset == "USDT" && decimal.RequireFromString(b.Free).Equal(decimal.NewFromInt(10000)) && b.ValuationUsd != nil && *b.ValuationUsd == "10000.00" {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("read %d: funding USDT 10000 missing from overview: %+v", i, ov.Accounts)
		}
		var fb paginatedBalances
		do("/assets/balances?pageSize=100&account=funding", &fb)
		ok := false
		for _, b := range fb.Items {
			ok = ok || (b.Asset == "USDT" && decimal.RequireFromString(b.Free).Equal(decimal.NewFromInt(10000)))
		}
		if !ok {
			t.Fatalf("read %d: /assets/balances?account=funding missing USDT 10000: %+v", i, fb.Items)
		}
	}
}
