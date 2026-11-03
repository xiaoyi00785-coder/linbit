package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

func newTestStore(t *testing.T) *Store {
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
	return NewStore(db)
}

func testUser() string { return fmt.Sprintf("u%d", time.Now().UnixNano()) }

// TestSymbolConfig checks the seeded default trading pairs load with their rules.
func TestSymbolConfig(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sym, err := store.Symbol(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("symbol: %v", err)
	}
	if sym.Base != "BTC" || sym.Quote != "USDT" || sym.Status != "trading" {
		t.Fatalf("unexpected symbol: %+v", sym)
	}
	if sym.PricePrecision != 2 || sym.QuantityPrecision != 6 {
		t.Fatalf("unexpected precision: %+v", sym)
	}
	if _, err := store.Symbol(ctx, "NOPEUSDT"); err != ErrSymbolNotFound {
		t.Fatalf("missing symbol: got %v, want ErrSymbolNotFound", err)
	}
}

// TestOrderLifecycle exercises insert, fill update, trade recording, the
// Consumed helper, the one-shot unfreeze claim, and the query endpoints.
func TestOrderLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	uid := testUser()

	id, err := store.NextOrderID(ctx)
	if err != nil {
		t.Fatalf("next id: %v", err)
	}
	o := &Order{
		OrderID:        id,
		UserID:         uid,
		Symbol:         "BTCUSDT",
		Side:           "buy",
		Type:           "limit",
		TimeInForce:    "GTC",
		Price:          decimal.RequireFromString("70000"),
		HasPrice:       true,
		Quantity:       decimal.RequireFromString("0.01"),
		FilledQuantity: decimal.Zero,
		Status:         "new",
		FrozenAsset:    "USDT",
		FrozenAmount:   decimal.RequireFromString("700"),
	}
	if err := store.InsertOrder(ctx, o); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	// Open orders should include it.
	open, total, err := store.OpenOrders(ctx, uid, "BTCUSDT", 1, 20)
	if err != nil || total != 1 || len(open) != 1 {
		t.Fatalf("open orders: total=%d err=%v", total, err)
	}

	// Record a fill of 0.004 @ 70000 and update the order to partially_filled.
	if err := store.InsertTrade(ctx, TradeRow{
		TradeID: "t-" + id, OrderID: id, UserID: uid, Symbol: "BTCUSDT", Side: "buy",
		Price: decimal.RequireFromString("70000"), Quantity: decimal.RequireFromString("0.004"),
		Fee: decimal.RequireFromString("0.000004"), FeeAsset: "BTC", Role: "taker",
	}); err != nil {
		t.Fatalf("insert trade: %v", err)
	}
	// Re-inserting the same (trade_id, order_id) must be a no-op.
	if err := store.InsertTrade(ctx, TradeRow{
		TradeID: "t-" + id, OrderID: id, UserID: uid, Symbol: "BTCUSDT", Side: "buy",
		Price: decimal.RequireFromString("70000"), Quantity: decimal.RequireFromString("0.004"),
		Fee: decimal.RequireFromString("0.000004"), FeeAsset: "BTC", Role: "taker",
	}); err != nil {
		t.Fatalf("insert trade (idempotent): %v", err)
	}

	consumed, err := store.Consumed(ctx, id, "buy")
	if err != nil {
		t.Fatalf("consumed: %v", err)
	}
	if !consumed.Equal(decimal.RequireFromString("280")) { // 0.004 * 70000
		t.Fatalf("consumed = %s, want 280", consumed)
	}

	if err := store.UpdateFill(ctx, id, decimal.RequireFromString("0.004"), "canceled"); err != nil {
		t.Fatalf("update fill: %v", err)
	}

	// First unfreeze claim wins; second loses.
	won, err := store.ClaimUnfreeze(ctx, id)
	if err != nil || !won {
		t.Fatalf("first claim: won=%v err=%v", won, err)
	}
	won, err = store.ClaimUnfreeze(ctx, id)
	if err != nil || won {
		t.Fatalf("second claim: won=%v err=%v (want false)", won, err)
	}

	// Now canceled, it drops out of open orders but shows in history + trades.
	open, _, err = store.OpenOrders(ctx, uid, "BTCUSDT", 1, 20)
	if err != nil || len(open) != 0 {
		t.Fatalf("open after cancel: len=%d err=%v", len(open), err)
	}
	hist, htotal, err := store.OrderHistory(ctx, uid, "BTCUSDT", "canceled", 1, 20)
	if err != nil || htotal != 1 || len(hist) != 1 {
		t.Fatalf("history: total=%d err=%v", htotal, err)
	}
	trades, ttotal, err := store.MyTrades(ctx, uid, "BTCUSDT", 1, 20)
	if err != nil || ttotal != 1 || len(trades) != 1 {
		t.Fatalf("my trades: total=%d err=%v", ttotal, err)
	}
}

// insertOrderWithStatus inserts a minimal order in the given status for tests.
func insertOrderWithStatus(t *testing.T, store *Store, uid, status string) string {
	t.Helper()
	ctx := context.Background()
	id, err := store.NextOrderID(ctx)
	if err != nil {
		t.Fatalf("next id: %v", err)
	}
	o := &Order{
		OrderID: id, UserID: uid, Symbol: "BTCUSDT", Side: "buy", Type: "limit",
		TimeInForce: "GTC", Price: decimal.RequireFromString("70000"), HasPrice: true,
		Quantity: decimal.RequireFromString("0.01"), FilledQuantity: decimal.Zero,
		Status: status, FrozenAsset: "USDT", FrozenAmount: decimal.RequireFromString("700"),
	}
	if err := store.InsertOrder(ctx, o); err != nil {
		t.Fatalf("insert %s order: %v", status, err)
	}
	if status != "new" {
		if err := store.UpdateFill(ctx, id, decimal.Zero, status); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
	}
	return id
}

// TestOrderHistoryTerminalOnly verifies history returns only terminal orders
// (filled/canceled/rejected/expired) and never live ones (new/partially_filled),
// even when a non-terminal status filter is supplied.
func TestOrderHistoryTerminalOnly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	uid := testUser()

	insertOrderWithStatus(t, store, uid, "new")
	insertOrderWithStatus(t, store, uid, "partially_filled")
	insertOrderWithStatus(t, store, uid, "filled")
	insertOrderWithStatus(t, store, uid, "canceled")
	insertOrderWithStatus(t, store, uid, "rejected")
	insertOrderWithStatus(t, store, uid, "expired")

	hist, total, err := store.OrderHistory(ctx, uid, "", "", 1, 50)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if total != 4 || len(hist) != 4 {
		t.Fatalf("history total=%d len=%d, want 4 (terminal only)", total, len(hist))
	}
	for _, o := range hist {
		if !terminal(o.Status) && o.Status != "expired" {
			t.Fatalf("history contained non-terminal status %q", o.Status)
		}
	}

	// A non-terminal status filter must match nothing.
	_, ptotal, err := store.OrderHistory(ctx, uid, "", "partially_filled", 1, 50)
	if err != nil {
		t.Fatalf("history filter: %v", err)
	}
	if ptotal != 0 {
		t.Fatalf("history with status=partially_filled total=%d, want 0", ptotal)
	}

	// A terminal status filter narrows to that status.
	_, ftotal, err := store.OrderHistory(ctx, uid, "", "filled", 1, 50)
	if err != nil {
		t.Fatalf("history filled filter: %v", err)
	}
	if ftotal != 1 {
		t.Fatalf("history with status=filled total=%d, want 1", ftotal)
	}
}
