package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

// newTestDCAServer sets up a real Postgres-backed Server with every outgoing
// HTTP dependency (symbol, place, orderInfo, lastPrice) stubbed. It is
// skipped (not failed) when DATABASE_URL is not set, so `go test ./...`
// stays green on a developer machine without Postgres.
func newTestDCAServer(t *testing.T) (*Server, *fakeOrderBook) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed DCA test")
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
	cfg := Config{
		DCAPoll:            50 * time.Millisecond,
		DCAMinInterval:     1,
		DCAMaxPlansPerUser: 10,
		DCAMaxCatchup:      1,
		FeeRate:            decimal.RequireFromString("0.001"),
	}
	srv := NewServer(cfg, db)
	book := &fakeOrderBook{price: decimal.RequireFromString("30000")}
	srv.symbolFn = func(ctx context.Context, sym string) (SymbolInfo, error) {
		return SymbolInfo{Symbol: sym, Base: "BTC", Quote: "USDT", PricePrecision: 2, QuantityPrecision: 6,
			MinQuantity: decimal.RequireFromString("0.0001"), MinNotional: decimal.RequireFromString("1"),
			Status: "trading"}, nil
	}
	srv.placeFn = book.place
	srv.orderInfoFn = book.get
	srv.marketPriceFn = func(ctx context.Context, sym string) (decimal.Decimal, error) {
		return book.price, nil
	}
	return srv, book
}

// fakeOrderBook stands in for the order service: every place() returns a
// unique order id and the next orderInfo() call surfaces the order as filled
// at the book's current price, honouring the requested quote (buy) or base
// (sell) amount.
type fakeOrderBook struct {
	mu    sync.Mutex
	price decimal.Decimal
	seq   atomic.Int64
	// calls counts how many place() calls landed (per plan-client-id prefix).
	placed int
	orders map[string]orderInfo
}

func (b *fakeOrderBook) place(ctx context.Context, uid string, r placeReq) (orderDTO, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.placed++
	id := fmt.Sprintf("fake-%d", b.seq.Add(1))
	if b.orders == nil {
		b.orders = map[string]orderInfo{}
	}
	var info orderInfo
	info.Order.Status = "filled"
	if r.Side == "buy" && r.QuoteOrderQty != nil {
		q := decimal.RequireFromString(*r.QuoteOrderQty)
		info.FilledQuantity = q.Div(b.price).Round(8)
		info.FilledQuote = q
	} else if r.Side == "sell" && r.Quantity != nil {
		qty := decimal.RequireFromString(*r.Quantity)
		info.FilledQuantity = qty
		info.FilledQuote = qty.Mul(b.price).Round(8)
	}
	b.orders[id] = info
	return orderDTO{OrderID: id, Status: "filled"}, nil
}

func (b *fakeOrderBook) get(ctx context.Context, uid, id string) (orderInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[id]
	if !ok {
		return orderInfo{}, fmt.Errorf("order %s not found", id)
	}
	return o, nil
}

func (b *fakeOrderBook) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.placed
}

func dcaUser() string { return fmt.Sprintf("u%d", time.Now().UnixNano()) }

// TestDCACreate covers the happy path and the main validation branches.
func TestDCACreate(t *testing.T) {
	srv, _ := newTestDCAServer(t)
	ctx := context.Background()
	uid := dcaUser()

	p, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "10", Interval: 60})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Status != planActive || p.TotalRuns != 0 {
		t.Fatalf("new plan state: %+v", p)
	}
	if p.Base != "BTC" || p.Quote != "USDT" {
		t.Fatalf("symbol split: %+v", p)
	}

	if _, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "10", Interval: 0}); err == nil {
		t.Fatalf("expected interval validation error")
	}
	if _, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "zap",
		BaseAmount: "10", Interval: 60}); err == nil {
		t.Fatalf("expected side validation error")
	}
	if _, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "0.001", Interval: 60}); err == nil {
		t.Fatalf("expected notional validation error")
	}
}

// TestDCAScheduleFires verifies a due active plan fires exactly once per tick
// and advances next_run_at by one interval.
func TestDCAScheduleFires(t *testing.T) {
	srv, book := newTestDCAServer(t)
	ctx := context.Background()
	uid := dcaUser()

	start := time.Now().Add(-time.Minute)
	srv.nowFn = func() time.Time { return start }
	p, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "10", Interval: 60, StartAt: stringPtr(start.Format(time.RFC3339))})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Freeze the clock at start; the plan is due at exactly start.
	srv.TickDCA(ctx)
	if book.count() != 1 {
		t.Fatalf("expected 1 placement, got %d", book.count())
	}
	// Second tick at the same instant: not due anymore (next_run_at moved +60s).
	srv.TickDCA(ctx)
	if book.count() != 1 {
		t.Fatalf("expected no second placement, got %d", book.count())
	}

	// Verify plan totals and execution row.
	got, err := srv.dcaPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TotalRuns != 1 {
		t.Fatalf("totalRuns = %d", got.TotalRuns)
	}
	want := decimal.RequireFromString("10")
	if !got.TotalQuote.Equal(want) {
		t.Fatalf("totalQuote = %s", got.TotalQuote)
	}
	if !got.TotalBase.Equal(want.Div(book.price).Round(8)) {
		t.Fatalf("totalBase = %s", got.TotalBase)
	}
	n, _ := srv.countExecutions(ctx, p.ID)
	if n != 1 {
		t.Fatalf("executions = %d", n)
	}
}

// TestDCAPauseAndResume: paused plans do NOT fire; resume re-arms but does not
// itself place an order (no double-send), and the next tick fires normally.
func TestDCAPauseAndResume(t *testing.T) {
	srv, book := newTestDCAServer(t)
	ctx := context.Background()
	uid := dcaUser()

	start := time.Now().Add(-time.Minute)
	srv.nowFn = func() time.Time { return start }
	p, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "10", Interval: 60, StartAt: stringPtr(start.Format(time.RFC3339))})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := srv.PauseDCA(ctx, uid, p.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	srv.TickDCA(ctx)
	if book.count() != 0 {
		t.Fatalf("paused plan fired: count=%d", book.count())
	}
	// Resume at the SAME instant: the plan was due then; resume clamps
	// next_run_at to now; the next tick fires it exactly once, not twice.
	before := book.count()
	if _, err := srv.ResumeDCA(ctx, uid, p.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if book.count() != before {
		t.Fatalf("resume placed order: %d -> %d", before, book.count())
	}
	srv.TickDCA(ctx)
	if book.count() != 1 {
		t.Fatalf("expected 1 placement post-resume, got %d", book.count())
	}
}

// TestDCADelete marks the plan stopped and the scheduler skips it afterwards.
func TestDCADelete(t *testing.T) {
	srv, book := newTestDCAServer(t)
	ctx := context.Background()
	uid := dcaUser()

	start := time.Now().Add(-time.Minute)
	srv.nowFn = func() time.Time { return start }
	p, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "10", Interval: 60, StartAt: stringPtr(start.Format(time.RFC3339))})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := srv.DeleteDCA(ctx, uid, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	srv.TickDCA(ctx)
	if book.count() != 0 {
		t.Fatalf("deleted plan fired")
	}
	got, err := srv.dcaPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Status != planStopped || !got.StoppedAt.Valid {
		t.Fatalf("not stopped: %+v", got)
	}
}

// TestDCAEndReached stops a plan whose end_at has passed instead of firing.
func TestDCAEndReached(t *testing.T) {
	srv, book := newTestDCAServer(t)
	ctx := context.Background()
	uid := dcaUser()

	past := time.Now().Add(-time.Hour)
	srv.nowFn = func() time.Time { return past.Add(time.Minute) } // start+1min
	start := past.Format(time.RFC3339)
	end := past.Add(30 * time.Second).Format(time.RFC3339)
	p, err := srv.CreateDCA(ctx, uid, dcaCreateReq{Symbol: "BTCUSDT", Side: "buy",
		BaseAmount: "10", Interval: 60, StartAt: &start, EndAt: &end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	srv.TickDCA(ctx)
	if book.count() != 0 {
		t.Fatalf("past end fired")
	}
	got, err := srv.dcaPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Status != planStopped {
		t.Fatalf("not stopped: %+v", got)
	}
}

// TestDCANextRun confirms nextRun advances by the interval and catches up to
// now without exploding, capped by maxCatchup.
func TestDCANextRun(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	p := DCAPlan{NextRunAt: now.Add(-5 * time.Minute), IntervalSec: 60}
	// With catchup=1, the loop advances to the first future tick.
	next := nextRun(p, now, 1)
	if !next.After(now) {
		t.Fatalf("next not in future: %s (now %s)", next, now)
	}
	// With a huge catchup the result is bounded by the inner loop guard so it
	// still returns in the future, not a value wedged in the past.
	next = nextRun(p, now, 100)
	if !next.After(now) {
		t.Fatalf("next not in future: %s (now %s)", next, now)
	}
}

func stringPtr(s string) *string { return &s }
