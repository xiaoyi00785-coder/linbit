package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestBalancesAccountParam(t *testing.T) {
	for in, want := range map[string]string{"": AcctSpot, "spot": AcctSpot, "funding": AcctFunding, " Funding ": AcctFunding, "FUNDING": AcctFunding} {
		if got, ok := balancesAccount(in); !ok || got != want {
			t.Fatalf("balancesAccount(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"futures", "margin", "x"} {
		if _, ok := balancesAccount(bad); ok {
			t.Fatalf("balancesAccount(%q) must be rejected", bad)
		}
	}
}

func TestDefaultAssetsListed(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range defaultAssets {
		seen[a] = true
	}
	for _, a := range []string{"USDT", "USDC", "BTC", "ETH", "SOL", "BNB", "TRX"} {
		if !seen[a] {
			t.Fatalf("default asset list missing %s", a)
		}
	}
}

// DB-backed: funding vs spot are distinguishable and both list every default asset.
func TestBalancesFundingVsSpot(t *testing.T) {
	store, db := newTestStore(t)
	h := NewServer(Config{InternalToken: "secret"}, store).Routes()
	uid := testUser()
	if _, err := db.Exec(`INSERT INTO funding_balances (user_id, asset, free) VALUES ($1,'SOL',3)`, uid); err != nil {
		t.Fatal(err)
	}
	get := func(q string) paginatedBalances {
		r := httptest.NewRequest("GET", "/assets/balances?pageSize=100"+q, nil)
		r.Header.Set("X-User-Id", uid)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", q, w.Code, w.Body.String())
		}
		var p paginatedBalances
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		return p
	}
	spot, fund := get(""), get("&account=funding")
	if spot.Account != AcctSpot || fund.Account != AcctFunding {
		t.Fatalf("account fields = %q/%q", spot.Account, fund.Account)
	}
	if spot.Total != len(defaultAssets) || fund.Total != len(defaultAssets) {
		t.Fatalf("totals spot=%d funding=%d want %d", spot.Total, fund.Total, len(defaultAssets))
	}
	for _, it := range fund.Items {
		if it.Asset == "SOL" && it.Free != "3.00000000" {
			t.Fatalf("funding SOL = %s", it.Free)
		}
	}
	for _, it := range spot.Items {
		if it.Asset == "SOL" && it.Free != "0.00000000" {
			t.Fatalf("spot SOL leaked funding balance: %s", it.Free)
		}
	}
}
