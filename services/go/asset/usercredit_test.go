package main

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func creditEnv(t *testing.T) {
	for _, k := range []string{"LINBIT_NETWORK", "NETWORK", "APP_ENV", "LINBIT_MAINNET", "MAINNET"} {
		t.Setenv(k, "")
	}
	t.Setenv("WALLET_CHAIN", "testnet")
	t.Setenv("ADMIN_USER_CREDIT_ENABLED", "true")
	t.Setenv("ADMIN_USER_CREDIT_MAX", "100000")
	t.Setenv("ADMIN_USER_CREDIT_DAILY_MAX", "1000000000000")
}

func TestUserCreditMainnetRefusedNoDB(t *testing.T) {
	creditEnv(t)
	s := &Store{} // nil db: must refuse before touching the database
	for _, env := range [][2]string{{"APP_ENV", "mainnet"}, {"NETWORK", "mainnet"}, {"LINBIT_MAINNET", "true"}, {"WALLET_CHAIN", "mainnet"}, {"WALLET_CHAIN", "mock"}, {"ADMIN_USER_CREDIT_ENABLED", "false"}} {
		t.Run(env[0]+"="+env[1], func(t *testing.T) {
			t.Setenv(env[0], env[1])
			_, err := s.AdminTestnetCredit(context.Background(), "u1", "USDT", decimal.NewFromInt(1), "r1")
			if !errors.Is(err, ErrCreditDisabled) {
				t.Fatalf("want ErrCreditDisabled, got %v", err)
			}
		})
	}
}

func TestUserCreditIdempotentAndLedger(t *testing.T) {
	creditEnv(t)
	store, db := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	ref := "qa-" + uid
	amt := decimal.RequireFromString("10000")

	r1, err := store.AdminTestnetCredit(ctx, uid, "usdt", amt, ref)
	if err != nil || r1.Replayed || r1.Before != "0" || r1.After != "10000" {
		t.Fatalf("first credit: %+v %v", r1, err)
	}
	r2, err := store.AdminTestnetCredit(ctx, uid, "USDT", amt, ref)
	if err != nil || !r2.Replayed || r2.Amount != "10000" {
		t.Fatalf("replay: %+v %v", r2, err)
	}
	free, err := store.fundingFree(ctx, db, uid, "USDT")
	if err != nil || !free.Equal(amt) {
		t.Fatalf("funding free = %s (%v), want 10000 (credited once)", free, err)
	}
	if _, err := store.AdminTestnetCredit(ctx, uid, "USDT", decimal.RequireFromString("1"), ref); !errors.Is(err, ErrRefConflict) {
		t.Fatalf("different amount same ref: %v", err)
	}
	if _, err := store.AdminTestnetCredit(ctx, uid+"x", "USDT", amt, ref); !errors.Is(err, ErrRefConflict) {
		t.Fatalf("different user same ref: %v", err)
	}
	// Balanced double entry against the testnet treasury.
	var debit, credit string
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN direction='debit' THEN amount END),0)::text,
		       COALESCE(SUM(CASE WHEN direction='credit' AND account=$2 THEN amount END),0)::text
		FROM ledger_entries WHERE tx_id IN (SELECT id FROM ledger_transactions WHERE ref=$1)`, creditRef(ref), TestnetTreasuryAccount).Scan(&debit, &credit); err != nil {
		t.Fatalf("ledger query: %v", err)
	}
	if !decimal.RequireFromString(debit).Equal(amt) || !decimal.RequireFromString(credit).Equal(amt) {
		t.Fatalf("ledger debit=%s credit=%s", debit, credit)
	}
}

func TestUserCreditCaps(t *testing.T) {
	creditEnv(t)
	store, db := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	if _, err := store.AdminTestnetCredit(ctx, uid, "USDT", decimal.RequireFromString("100000.01"), "cap-"+uid); !errors.Is(err, ErrCreditCap) {
		t.Fatalf("per-credit cap: %v", err)
	}
	if _, err := store.AdminTestnetCredit(ctx, uid, "BTC", decimal.NewFromInt(1), "asset-"+uid); !errors.Is(err, ErrCreditAsset) {
		t.Fatalf("asset: %v", err)
	}
	if _, err := store.AdminTestnetCredit(ctx, mmUserID(), "USDT", decimal.NewFromInt(1), "mm-"+uid); !errors.Is(err, ErrCreditMMAccount) {
		t.Fatalf("mm account: %v", err)
	}
	var today string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0)::text FROM wallet_ops WHERE op=$1 AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`, userCreditOp).Scan(&today); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_USER_CREDIT_DAILY_MAX", decimal.RequireFromString(today).Add(decimal.NewFromInt(50)).String())
	if _, err := store.AdminTestnetCredit(ctx, uid, "USDT", decimal.NewFromInt(40), "d1-"+uid); err != nil {
		t.Fatalf("within daily cap: %v", err)
	}
	if _, err := store.AdminTestnetCredit(ctx, uid, "USDT", decimal.NewFromInt(20), "d2-"+uid); !errors.Is(err, ErrCreditDailyCap) {
		t.Fatalf("daily cap: %v", err)
	}
}
