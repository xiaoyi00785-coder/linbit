package main

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

// wallet_ops rows written before refs were namespaced by op carry the bare ref.
// Retrying one of those operations must not apply it a second time.
func TestClaimRefHonoursBareLegacyRows(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	d := decimal.RequireFromString
	ref := "legacy-dep-" + uid

	if err := store.EnsureAccount(ctx, uid); err != nil {
		t.Fatal(err)
	}
	// A deposit recorded the old way: bare ref.
	if _, err := db.ExecContext(ctx, `INSERT INTO wallet_ops (ref, op, user_id, asset, amount) VALUES ($1, 'deposit', $2, 'USDT', '50')`, ref, uid); err != nil {
		t.Fatal(err)
	}
	if err := store.Deposit(ctx, uid, "USDT", d("50"), ref); !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("retry of a legacy deposit: got %v, want ErrAlreadyApplied", err)
	}
	if bal, err := store.LedgerBalance(ctx, uid, "USDT"); err != nil || !bal.IsZero() {
		t.Fatalf("legacy retry moved money: balance %s %v", bal, err)
	}
	// Another op may still reuse the caller's ref: that is why refs are namespaced.
	other := "shared-" + uid
	if err := store.Deposit(ctx, uid, "USDT", d("10"), other); err != nil {
		t.Fatalf("deposit: %v", err)
	}
	if err := store.Deposit(ctx, uid, "USDT", d("10"), other); !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("second deposit with the same ref: %v", err)
	}
	if bal, err := store.LedgerBalance(ctx, uid, "USDT"); err != nil || !bal.Equal(d("10")) {
		t.Fatalf("balance %s %v, want 10", bal, err)
	}
}

// An admin adjustment recorded under the bare "admin_adjust:<ref>" key replays
// instead of conflicting, and its ref cannot be reused with the opposite sign.
func TestAdminAdjustLegacyKeyAndSignConflict(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	d := decimal.RequireFromString

	// New-style row: credit, then the same ref as a debit must conflict.
	if _, err := store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("30"), "n-"+uid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("-30"), "n-"+uid); !errors.Is(err, ErrRefConflict) {
		t.Fatalf("debit under a credit's ref: got %v, want ErrRefConflict", err)
	}
	if bal, err := store.LedgerBalance(ctx, uid, "USDT"); err != nil || !bal.Equal(d("30")) {
		t.Fatalf("balance %s %v, want 30 (debit must not apply)", bal, err)
	}

	// Old-style row: rewrite the stored key to the bare form used before the
	// namespacing, then replay.
	key := adjustRef("n-" + uid)
	if res, err := db.ExecContext(ctx, `UPDATE wallet_ops SET ref=$1 WHERE ref=$2`, key, storedRef(adminAdjustCredit, key)); err != nil {
		t.Fatal(err)
	} else if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("expected one stored row for %s, got %d", key, n)
	}
	res, err := store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("30"), "n-"+uid)
	if err != nil || !res.Replayed {
		t.Fatalf("replay of a legacy row: %+v %v", res, err)
	}
	if _, err := store.AdminAdjust(ctx, uid, "USDT", AcctSpot, d("-30"), "n-"+uid); !errors.Is(err, ErrRefConflict) {
		t.Fatalf("debit under a legacy credit's ref: got %v, want ErrRefConflict", err)
	}
	if bal, err := store.LedgerBalance(ctx, uid, "USDT"); err != nil || !bal.Equal(d("30")) {
		t.Fatalf("balance %s %v, want 30", bal, err)
	}
}
