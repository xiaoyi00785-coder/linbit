package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

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
	return NewStore(db), db
}

// testUserSeq keeps ids unique even when the clock is coarse: macOS reports
// time in microseconds, so back-to-back UnixNano() calls can return the same
// value and two "different" test users would collide.
var testUserSeq atomic.Int64

func testUser() string {
	return fmt.Sprintf("u%d%04d", time.Now().UnixNano(), testUserSeq.Add(1)%10000)
}

func TestCreditFreezeUnfreeze(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	uid := testUser()

	if err := store.EnsureAccount(ctx, uid); err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	if err := store.Credit(ctx, uid, "USDT", decimal.RequireFromString("1000"), "deposit-1"); err != nil {
		t.Fatalf("credit: %v", err)
	}
	if err := store.Freeze(ctx, uid, "USDT", decimal.RequireFromString("250"), "order-1"); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	bals, _, err := store.Balances(ctx, uid, 1, 20)
	if err != nil {
		t.Fatalf("balances: %v", err)
	}
	usdt := findAsset(bals, "USDT")
	if usdt == nil || !usdt.Free.Equal(decimal.RequireFromString("750")) || !usdt.Locked.Equal(decimal.RequireFromString("250")) {
		t.Fatalf("after freeze got %+v, want free=750 locked=250", usdt)
	}

	if err := store.Unfreeze(ctx, uid, "USDT", decimal.RequireFromString("100"), "order-1-cancel"); err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	bals, _, _ = store.Balances(ctx, uid, 1, 20)
	usdt = findAsset(bals, "USDT")
	if !usdt.Free.Equal(decimal.RequireFromString("850")) || !usdt.Locked.Equal(decimal.RequireFromString("150")) {
		t.Fatalf("after unfreeze got %+v, want free=850 locked=150", usdt)
	}

	// Ledger must reconcile with the materialised balance (debits-credits over
	// the user's buckets == free + locked).
	ledger, err := store.LedgerBalance(ctx, uid, "USDT")
	if err != nil {
		t.Fatalf("ledger balance: %v", err)
	}
	if !ledger.Equal(usdt.Total()) {
		t.Fatalf("ledger %s != materialised total %s", ledger, usdt.Total())
	}
}

// TestWithdrawFreezeUnfreeze verifies that a withdrawal's freeze and unfreeze
// (reject/cancel path) use distinct refs and do not collide.
func TestWithdrawFreezeUnfreeze(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	withdrawID := "withdraw:123"

	if err := store.EnsureAccount(ctx, uid); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := store.Credit(ctx, uid, "USDT", decimal.RequireFromString("1000"), "seed-"+uid); err != nil {
		t.Fatalf("credit: %v", err)
	}

	// Freeze for withdrawal (same as FreezeWithdraw, but at the store level).
	if err := store.Freeze(ctx, uid, "USDT", decimal.RequireFromString("500"), withdrawID); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	assertBalance(t, store, uid, "USDT", "500", "500")

	// Withdrawal rejected/cancelled: unfreeze must succeed with the same ref.
	if err := store.Unfreeze(ctx, uid, "USDT", decimal.RequireFromString("500"), withdrawID); err != nil {
		t.Fatalf("unfreeze after reject: %v", err)
	}
	assertBalance(t, store, uid, "USDT", "1000", "0")

	// Idempotency: repeating the unfreeze with the same ref is a no-op.
	if err := store.Unfreeze(ctx, uid, "USDT", decimal.RequireFromString("500"), withdrawID); err != ErrAlreadyApplied {
		t.Fatalf("second unfreeze: got %v, want ErrAlreadyApplied", err)
	}
	assertBalance(t, store, uid, "USDT", "1000", "0")
}

// TestWithdrawFreezeComplete verifies that completing a withdrawal after freezing
// does not collide (the freeze and complete operations namespace their refs).
func TestWithdrawFreezeComplete(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	feeAcct := testUser()
	withdrawID := "withdraw:456"

	if err := store.EnsureAccount(ctx, uid); err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	if err := store.EnsureAccount(ctx, feeAcct); err != nil {
		t.Fatalf("ensure fee: %v", err)
	}
	if err := store.Credit(ctx, uid, "USDT", decimal.RequireFromString("1000"), "seed-"+uid); err != nil {
		t.Fatalf("credit: %v", err)
	}

	// Freeze for withdrawal.
	if err := store.Freeze(ctx, uid, "USDT", decimal.RequireFromString("500"), withdrawID); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	assertBalance(t, store, uid, "USDT", "500", "500")

	// Complete the withdrawal (deduct locked balance, collect gas fee).
	if err := store.WithdrawComplete(ctx, uid, "USDT", decimal.RequireFromString("490"), decimal.RequireFromString("10"), feeAcct, withdrawID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	assertBalance(t, store, uid, "USDT", "500", "0")
	assertBalance(t, store, feeAcct, "USDT", "10", "0")

	// Idempotency: repeating complete with the same ref is a no-op.
	if err := store.WithdrawComplete(ctx, uid, "USDT", decimal.RequireFromString("490"), decimal.RequireFromString("10"), feeAcct, withdrawID); err != ErrAlreadyApplied {
		t.Fatalf("second complete: got %v, want ErrAlreadyApplied", err)
	}
	assertBalance(t, store, uid, "USDT", "500", "0")
	assertBalance(t, store, feeAcct, "USDT", "10", "0")
}

func TestFreezeInsufficient(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	if err := store.EnsureAccount(ctx, uid); err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	// Freezing more than the (zero) free balance must fail.
	if err := store.Freeze(ctx, uid, "USDT", decimal.RequireFromString("10"), "x"); err != ErrInsufficient {
		t.Fatalf("freeze insufficient: got %v, want ErrInsufficient", err)
	}
	bals, _, _ := store.Balances(ctx, uid, 1, 20)
	usdt := findAsset(bals, "USDT")
	if !usdt.Free.IsZero() || !usdt.Locked.IsZero() {
		t.Fatalf("balance changed after failed freeze: %+v", usdt)
	}
}

// TestConcurrentFreeze exercises row locking: many parallel freezes against a
// limited balance must never over-withdraw (free never goes negative).
func TestConcurrentFreeze(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	uid := testUser()
	if err := store.EnsureAccount(ctx, uid); err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	if err := store.Credit(ctx, uid, "USDT", decimal.RequireFromString("100"), fmt.Sprintf("seed-%s", uid)); err != nil {
		t.Fatalf("credit: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 20; i++ { // 20 x 10 = 200 requested against 100 available
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := store.Freeze(ctx, uid, "USDT", decimal.RequireFromString("10"), fmt.Sprintf("c-%d", i))
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if success != 10 {
		t.Fatalf("expected exactly 10 successful freezes, got %d", success)
	}
	bals, _, _ := store.Balances(ctx, uid, 1, 20)
	usdt := findAsset(bals, "USDT")
	if !usdt.Free.IsZero() || !usdt.Locked.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("after concurrent freeze got %+v, want free=0 locked=100", usdt)
	}
}

func TestBalancesHandlerRequiresUser(t *testing.T) {
	store, _ := newTestStore(t)
	srv := NewServer(Config{InternalToken: "secret"}, store)
	h := srv.Routes()

	// No X-User-Id -> 401.
	r := httptest.NewRequest("GET", "/assets/balances", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing user status = %d, want 401", w.Code)
	}

	// With X-User-Id -> 200 and seeded default assets.
	uid := testUser()
	r = httptest.NewRequest("GET", "/assets/balances", nil)
	r.Header.Set("X-User-Id", uid)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("balances status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp paginatedBalances
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != len(defaultAssets) {
		t.Fatalf("seeded assets = %d, want %d", resp.Total, len(defaultAssets))
	}
}

func findAsset(bals []Balance, asset string) *Balance {
	for i := range bals {
		if bals[i].Asset == asset {
			return &bals[i]
		}
	}
	return nil
}
