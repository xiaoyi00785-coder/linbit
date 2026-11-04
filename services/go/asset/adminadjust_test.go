package main

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestAdminAdjust(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	d := decimal.RequireFromString
	adjNet0, err := store.AdminAdjustNet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, err := store.AdminAdjust(ctx, uid, "usdt", AcctSpot, d("100"), "c1-"+uid)
	if err != nil || res.Before != "0" || res.After != "100" || res.Replayed {
		t.Fatalf("credit: %+v %v", res, err)
	}
	// idempotent replay, and conflicts on changed params
	if res, err = store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("100"), "c1-"+uid); err != nil || !res.Replayed {
		t.Fatalf("replay: %+v %v", res, err)
	}
	for _, c := range []struct {
		acct string
		amt  string
	}{{AcctSpot, "101"}, {AcctSpot, "-100"}, {AcctFunding, "100"}} {
		if _, err := store.AdminAdjust(ctx, uid, "USDT", c.acct, d(c.amt), "c1-"+uid); !errors.Is(err, ErrRefConflict) {
			t.Fatalf("%v: want ref conflict, got %v", c, err)
		}
	}
	// debit can't go negative, and leaves nothing behind
	if _, err := store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("-100.00000001"), "d1-"+uid); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("want insufficient, got %v", err)
	}
	if res, err = store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("-40"), "d1-"+uid); err != nil || res.After != "60" {
		t.Fatalf("debit after failed attempt (ref must be reusable): %+v %v", res, err)
	}
	if _, err := store.AdminAdjust(ctx, uid, "USDT", AcctFunding, d("-1"), "d2-"+uid); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("funding debit: %v", err)
	}
	if bal, err := store.LedgerBalance(ctx, uid, "USDT"); err != nil || !bal.Equal(d("60")) {
		t.Fatalf("ledger balance %s %v", bal, err)
	}
	for _, bad := range []string{"system:x", "mm:main", "platform:fee"} {
		if _, err := store.AdminAdjust(ctx, bad, "USDT", AcctSpot, d("1"), "x-"+uid); !errors.Is(err, ErrAdjustAccount) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if _, err := store.AdminAdjust(ctx, uid, "USDT", "futures", d("1"), "x-"+uid); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("futures: %v", err)
	}
	adjNet, err := store.AdminAdjustNet(ctx)
	if err != nil || !adjNet["USDT"].Sub(adjNet0["USDT"]).Equal(d("60")) {
		t.Fatalf("net %v %v", adjNet, err)
	}
	liabs, err := store.Liabilities(ctx, "mm:main", "system:hedge")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range liabs {
		if l.Asset == "USDT" && !d(l.AdminAdjustNet).Equal(adjNet["USDT"]) {
			t.Fatalf("liability adminAdjustNet %s want %s", l.AdminAdjustNet, adjNet["USDT"])
		}
	}
}
