package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

// newTestStore opens DATABASE_URL (set by the devstack) and migrates. Tests skip
// when no database is configured, mirroring the asset/order service tests.
func newTestStore(t *testing.T) (*Store, *sql.DB) {
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
	// Isolate tests: ProcessWithdrawals/Scan operate over every row in the DB, so
	// leftovers from a previous run would be picked up here. The devstack DB is
	// shared by all modules, but these tables are wallet-only.
	if _, err := db.Exec(`TRUNCATE chain_txs, chain_state, withdrawals, deposits, deposit_addresses RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate wallet tables: %v", err)
	}
	return NewStore(db), db
}

func testUser() string { return fmt.Sprintf("w%d", time.Now().UnixNano()) }

// TestWithdrawalStateMachine exercises the allowed transitions and asserts that
// disallowed ones are rejected.
func TestWithdrawalStateMachine(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	user := testUser()

	newWd := func() int64 {
		id, err := store.InsertWithdrawal(ctx, Withdrawal{
			UserID: user, Asset: "USDT", Network: "TRC20", Address: "TDest",
			Amount: d("100"), GasFee: d("2"), GasFeeAsset: "USDT", GasDeductMode: "extra",
			FrozenAmount: d("102"), Status: wStatusReview,
		})
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		return id
	}

	// auto / manual release: review -> approved -> processing -> completed.
	id := newWd()
	if ok, err := store.Approve(ctx, id); !ok || err != nil {
		t.Fatalf("approve: ok=%v err=%v", ok, err)
	}
	if ok, _ := store.Approve(ctx, id); ok {
		t.Fatal("approve on an approved row should not apply")
	}
	if ok, err := store.MarkProcessing(ctx, id, "0xabc", 42); !ok || err != nil {
		t.Fatalf("mark processing: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkCompleted(ctx, id); !ok || err != nil {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}
	if ok, _ := store.MarkCompleted(ctx, id); ok {
		t.Fatal("completing twice should not apply")
	}
	wd, _ := store.GetWithdrawal(ctx, id)
	if wd.Status != wStatusCompleted || wd.TxHash.String != "0xabc" {
		t.Fatalf("final = %+v, want completed with tx 0xabc", wd)
	}

	// reject: review -> rejected, and then cannot be approved.
	id = newWd()
	if ok, err := store.Reject(ctx, id, "aml"); !ok || err != nil {
		t.Fatalf("reject: ok=%v err=%v", ok, err)
	}
	if ok, _ := store.Approve(ctx, id); ok {
		t.Fatal("approve after reject should not apply")
	}
	wd, _ = store.GetWithdrawal(ctx, id)
	if wd.Status != wStatusRejected || wd.RejectReason.String != "aml" {
		t.Fatalf("rejected row = %+v", wd)
	}

	// cancel: review -> canceled.
	id = newWd()
	if ok, err := store.Cancel(ctx, id); !ok || err != nil {
		t.Fatalf("cancel: ok=%v err=%v", ok, err)
	}

	// fail from approved.
	id = newWd()
	if ok, _ := store.Approve(ctx, id); !ok {
		t.Fatal("approve before fail")
	}
	if ok, err := store.MarkFailed(ctx, id, "broadcast_failed"); !ok || err != nil {
		t.Fatalf("mark failed: ok=%v err=%v", ok, err)
	}
}

// TestInjectIdempotent verifies a duplicate on-chain injection is a no-op.
func TestInjectIdempotent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	net := "TST" + testUser()
	tx := "0xdup"

	first, err := store.InjectChainTx(ctx, net, "Taddr", "USDT", d("10"), tx, 0)
	if err != nil || !first {
		t.Fatalf("first inject = %v/%v, want true", first, err)
	}
	second, err := store.InjectChainTx(ctx, net, "Taddr", "USDT", d("10"), tx, 0)
	if err != nil || second {
		t.Fatalf("duplicate inject = %v/%v, want false", second, err)
	}
}

// TestScannerCreditsOnce confirms the scanner credits a deposit exactly once,
// even when run repeatedly after confirmations are reached.
func TestScannerCreditsOnce(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	user := testUser()
	netName := "SCN" + testUser()

	net := Network{Asset: "USDT", Network: netName, Confirmations: 3, MinDeposit: d("1"),
		MinWithdraw: d("12"), MaxWithdraw: d("20000"), GasFeeEstimate: d("1"), GasFeeAsset: "USDT"}
	registry := newRegistry([]Network{net})

	addr, _ := store.UpsertDepositAddress(ctx, user, "USDT", netName, func() (string, string) {
		return "Tscanaddr", ""
	})
	if addr.Address != "Tscanaddr" {
		t.Fatalf("address = %q", addr.Address)
	}

	// Fake asset HTTP server counting deposit posts.
	hits := &fakeAssetHits{}
	srv := newFakeAssetFull(t, hits)
	asset := NewAssetClient(srv.URL, "tok")

	sc := NewScanner(store, registry, asset, true)

	if _, err := store.InjectChainTx(ctx, netName, "Tscanaddr", "USDT", d("25.5"), "0xsc", 0); err != nil {
		t.Fatalf("inject: %v", err)
	}
	// Not enough confirmations yet (height 1, need 3): no credit.
	if n, err := sc.ScanNetwork(ctx, netName); err != nil || n != 0 {
		t.Fatalf("pre-confirm scan = %d/%v, want 0", n, err)
	}
	if got := atomic.LoadInt32(&hits.deposit); got != 0 {
		t.Fatalf("credited before confirmations: %d calls", got)
	}
	// Advance to 3 confirmations.
	if _, err := store.AdvanceChain(ctx, netName, 2); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if n, err := sc.ScanNetwork(ctx, netName); err != nil || n != 1 {
		t.Fatalf("confirm scan = %d/%v, want 1", n, err)
	}
	// Re-scan repeatedly: still only one credit.
	for i := 0; i < 3; i++ {
		if n, _ := sc.ScanNetwork(ctx, netName); n != 0 {
			t.Fatalf("re-scan credited again: %d", n)
		}
	}
	if got := atomic.LoadInt32(&hits.deposit); got != 1 {
		t.Fatalf("asset deposit calls = %d, want 1", got)
	}
	status, _, _ := store.DepositStatus(ctx, netName, "0xsc", 0)
	if status != depCompleted {
		t.Fatalf("deposit status = %q, want completed", status)
	}
}

var _ = decimal.Zero
