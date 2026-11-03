package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
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

func testSymbol() string { return fmt.Sprintf("T%dUSDT", time.Now().UnixNano()) }

func TestKlinePersistAndRecent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sym := testSymbol()

	c1 := newCandle(1000, dec("100"), dec("1"))
	c1.Closed = true
	c2 := newCandle(1060, dec("101"), dec("2"))
	c2.add(dec("103"), dec("1")) // high 103, close 103, vol 3
	if err := store.UpsertKline(ctx, sym, "1m", c1); err != nil {
		t.Fatalf("upsert c1: %v", err)
	}
	if err := store.UpsertKline(ctx, sym, "1m", c2); err != nil {
		t.Fatalf("upsert c2: %v", err)
	}
	// Re-upsert c2 with more volume (idempotent update on the same bucket).
	c2.add(dec("104"), dec("1")) // vol 4, high 104, close 104
	if err := store.UpsertKline(ctx, sym, "1m", c2); err != nil {
		t.Fatalf("re-upsert c2: %v", err)
	}

	got, err := store.RecentKlines(ctx, sym, "1m", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(got))
	}
	if got[0].OpenTime != 1000 || got[1].OpenTime != 1060 {
		t.Fatalf("order wrong: %d, %d", got[0].OpenTime, got[1].OpenTime)
	}
	if got[1].High.String() != "104" || got[1].Close.String() != "104" || got[1].Volume.String() != "4" {
		t.Fatalf("updated candle = %+v", got[1])
	}
}

func TestRecoverReloadsKlines(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sym := testSymbol()

	c := newCandle(2000, dec("50"), dec("2"))
	c.Closed = true
	if err := store.UpsertKline(ctx, sym, "5m", c); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	agg := NewAggregator(store, []string{sym})
	if err := agg.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	ks, ok := agg.Klines(sym, "5m", 10)
	if !ok || len(ks) != 1 || ks[0].Time != 2000 || ks[0].Close != "50" {
		t.Fatalf("recovered klines = %v ok=%v", ks, ok)
	}
	// lastPrice carried over so the ticker has a price after restart.
	tk, _ := agg.TickerFrame(sym)
	if tk.LastPrice != "50" {
		t.Fatalf("recovered ticker lastPrice = %s", tk.LastPrice)
	}
}
