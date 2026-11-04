package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/shopspring/decimal"
)

// Integration tests for the advanced-order features. They need an empty
// Postgres database: FUTURES_TEST_DATABASE_URL=postgres://... go test -run Advanced
func advServer(t *testing.T) (*Server, context.Context) {
	t.Helper()
	url := os.Getenv("FUTURES_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FUTURES_TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	// 00012 must roll back and re-apply cleanly
	if err := goose.Down(db, "migrations"); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	cfg := Config{DefaultLeverage: 10, DefaultMarginMode: "cross", MarketBand: decimal.RequireFromString("0.05"), FundingInterval: 8 * time.Hour,
		TPSLRetry: time.Second, ADLEnabled: true, MinTransfer: decimal.RequireFromString("0.01")}
	s, err := NewServer(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"u:a", "u:b", "u:c"} {
		if _, err := db.Exec(`INSERT INTO fut_accounts (user_id, balance) VALUES ($1, 100000)`, u); err != nil {
			t.Fatal(err)
		}
	}
	s.setMark("BTCUSDT", "100000")
	return s, ctx
}

func (s *Server) setMark(sym, px string) {
	s.pmu.Lock()
	s.prices[sym].Mark = decimal.RequireFromString(px)
	s.pmu.Unlock()
}

func mustCode(t *testing.T, err error, code string) {
	t.Helper()
	ae, ok := asAPI(err)
	if !ok || ae.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func place(t *testing.T, s *Server, ctx context.Context, uid string, r PlaceReq) Order {
	t.Helper()
	r.Symbol = "BTCUSDT"
	o, _, err := s.PlaceOrder(ctx, uid, r)
	if err != nil {
		t.Fatalf("place %+v: %v", r, err)
	}
	return o
}

func TestAdvancedPostOnlyAndAmend(t *testing.T) {
	s, ctx := advServer(t)
	place(t, s, ctx, "u:a", PlaceReq{Side: "sell", Type: "limit", Price: "100000", Qty: "0.01"})
	_, _, err := s.PlaceOrder(ctx, "u:b", PlaceReq{Symbol: "BTCUSDT", Side: "buy", Type: "limit", Price: "100000", Qty: "0.01", TIF: "POST_ONLY"})
	mustCode(t, err, "POST_ONLY_REJECTED")
	b := place(t, s, ctx, "u:b", PlaceReq{Side: "buy", Type: "limit", Price: "99000", Qty: "0.02", TIF: "POST_ONLY"})
	seq := func(id int64) (v int64) {
		_ = s.db.QueryRow(`SELECT seq FROM fut_orders WHERE id=$1`, id).Scan(&v)
		return
	}
	s0 := seq(b.ID)
	o, _, err := s.AmendOrder(ctx, "u:b", b.ID, AmendReq{Qty: "0.01"})
	if err != nil || !o.Qty.Equal(decimal.RequireFromString("0.01")) || seq(b.ID) != s0 {
		t.Fatalf("qty decrease should keep priority: %v %s %d/%d", err, o.Qty, seq(b.ID), s0)
	}
	if o, _, err = s.AmendOrder(ctx, "u:b", b.ID, AmendReq{Price: "99500"}); err != nil || seq(b.ID) == s0 || o.AmendCount != 2 {
		t.Fatalf("price change should re-queue: %v", err)
	}
	_, _, err = s.AmendOrder(ctx, "u:b", b.ID, AmendReq{Price: "100000"})
	mustCode(t, err, "POST_ONLY_REJECTED")
	// GTC amend that crosses fills against the ask
	g := place(t, s, ctx, "u:c", PlaceReq{Side: "buy", Type: "limit", Price: "99000", Qty: "0.004"})
	o, tr, err := s.AmendOrder(ctx, "u:c", g.ID, AmendReq{Price: "100000"})
	if err != nil || len(tr) != 1 || o.Status != "filled" {
		t.Fatalf("crossing amend: %v %d %s", err, len(tr), o.Status)
	}
	// qty must stay above filled
	ask, _ := scanOrder(s.db.QueryRow(`SELECT ` + orderCols + ` FROM fut_orders WHERE user_id='u:a' AND status='partially_filled'`))
	_, _, err = s.AmendOrder(ctx, "u:a", ask.ID, AmendReq{Qty: "0.004"})
	mustCode(t, err, "INVALID_QTY")
	var frozen decimal.Decimal
	_ = s.db.QueryRow(`SELECT frozen FROM fut_accounts WHERE user_id='u:b'`).Scan(&frozen)
	if _, err := s.CancelOrder(ctx, "u:b", b.ID); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRow(`SELECT frozen FROM fut_accounts WHERE user_id='u:b'`).Scan(&frozen)
	if !frozen.IsZero() {
		t.Fatalf("reserve leaked after amend+cancel: %s", frozen)
	}
}

func TestAdvancedHedgeHistoryAlgo(t *testing.T) {
	s, ctx := advServer(t)
	mustCode(t, s.SetPositionMode(ctx, "u:b", "BOTH"), "VALIDATION_ERROR")
	if err := s.SetPositionMode(ctx, "u:b", "HEDGE"); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.PlaceOrder(ctx, "u:b", PlaceReq{Symbol: "BTCUSDT", Side: "buy", Type: "market", Qty: "0.01"})
	mustCode(t, err, "POSITION_SIDE_MISMATCH")
	// open LONG 0.02 and SHORT 0.01 at the same time
	place(t, s, ctx, "u:a", PlaceReq{Side: "sell", Type: "limit", Price: "100000", Qty: "0.02"})
	place(t, s, ctx, "u:b", PlaceReq{Side: "buy", Type: "market", Qty: "0.02", PositionSide: "LONG"})
	place(t, s, ctx, "u:a", PlaceReq{Side: "buy", Type: "limit", Price: "99900", Qty: "0.01"})
	place(t, s, ctx, "u:b", PlaceReq{Side: "sell", Type: "market", Qty: "0.01", PositionSide: "SHORT"})
	long, _, _ := loadPosition(ctx, s.db, "u:b", "BTCUSDT", "LONG", false)
	short, _, _ := loadPosition(ctx, s.db, "u:b", "BTCUSDT", "SHORT", false)
	if !long.Qty.Equal(decimal.RequireFromString("0.02")) || !short.Qty.Equal(decimal.RequireFromString("-0.01")) {
		t.Fatalf("hedge positions: long=%s short=%s", long.Qty, short.Qty)
	}
	mustCode(t, s.SetPositionMode(ctx, "u:b", "ONE_WAY"), "HAS_POSITION")
	st, _ := s.state(ctx, s.db, "u:b")
	if len(st.Positions) != 2 {
		t.Fatalf("want 2 position views, got %d", len(st.Positions))
	}
	// opening order may not be reduceOnly in hedge mode
	_, _, err = s.PlaceOrder(ctx, "u:b", PlaceReq{Symbol: "BTCUSDT", Side: "buy", Type: "market", Qty: "0.01", PositionSide: "LONG", ReduceOnly: true})
	mustCode(t, err, "VALIDATION_ERROR")

	// partial TP on LONG (MARK trigger): 0.005 of 0.02
	tp, err := s.CreateTPSL(ctx, "u:b", TPSLReq{Symbol: "BTCUSDT", PositionSide: "LONG", Qty: "0.005", TPTrigger: "101000", SLTrigger: "95000"})
	if err != nil || len(tp) != 2 || tp[0].TriggerType != "MARK" || tp[0].Dir != "up" || tp[1].Dir != "down" {
		t.Fatalf("create tpsl: %v %+v", err, tp)
	}
	if _, err := s.CancelAlgo(ctx, "u:b", tp[1].ID, []string{"tp", "sl"}); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateTPSL(ctx, "u:b", TPSLReq{Symbol: "BTCUSDT", PositionSide: "LONG", TPTrigger: "99000"})
	mustCode(t, err, "INVALID_TPSL")
	// plan order (LAST trigger) to add to SHORT when price rises to 102000
	plan, err := s.CreatePlan(ctx, "u:b", PlaceReq{Symbol: "BTCUSDT", Side: "sell", Type: "market", Qty: "0.003", PositionSide: "SHORT", TriggerPrice: "101500"})
	if err != nil || plan.Dir != "up" || plan.TriggerType != "LAST" {
		t.Fatalf("plan: %v %+v", err, plan)
	}
	mustCode(t, s.SetPositionMode(ctx, "u:b", "ONE_WAY"), "HAS_POSITION")

	// price moves up: liquidity for both triggers
	place(t, s, ctx, "u:c", PlaceReq{Side: "buy", Type: "limit", Price: "101000", Qty: "0.01"})
	s.setMark("BTCUSDT", "101600")
	s.setLast("BTCUSDT", []Trade{{Price: decimal.RequireFromString("101600")}})
	s.mu.Lock()
	s.triggerAlgos(ctx) // TP (MARK) fires; its fill moves LAST to 101000
	s.mu.Unlock()
	s.setLast("BTCUSDT", []Trade{{Price: decimal.RequireFromString("101600")}})
	s.mu.Lock()
	s.triggerAlgos(ctx) // plan (LAST) fires
	s.mu.Unlock()
	long, _, _ = loadPosition(ctx, s.db, "u:b", "BTCUSDT", "LONG", false)
	short, _, _ = loadPosition(ctx, s.db, "u:b", "BTCUSDT", "SHORT", false)
	if !long.Qty.Equal(decimal.RequireFromString("0.015")) || !short.Qty.Equal(decimal.RequireFromString("-0.013")) {
		t.Fatalf("after triggers: long=%s short=%s", long.Qty, short.Qty)
	}
	var st1, st2 string
	_ = s.db.QueryRow(`SELECT status FROM fut_algo_orders WHERE id=$1`, tp[0].ID).Scan(&st1)
	_ = s.db.QueryRow(`SELECT status FROM fut_algo_orders WHERE id=$1`, plan.ID).Scan(&st2)
	if st1 != "triggered" || st2 != "triggered" {
		t.Fatalf("algo statuses %s %s", st1, st2)
	}

	// close the LONG fully -> one history row
	place(t, s, ctx, "u:c", PlaceReq{Side: "buy", Type: "limit", Price: "101000", Qty: "0.015"})
	if _, _, err := s.ClosePosition(ctx, "u:b", "BTCUSDT", "LONG", "user"); err != nil {
		t.Fatal(err)
	}
	var n int
	var dir, entry, closePx, realized, maxQty string
	if err := s.db.QueryRow(`SELECT count(*) OVER (), direction, entry_price::text, close_price::text, realized_pnl::text, max_qty::text FROM fut_position_history WHERE user_id='u:b'`).
		Scan(&n, &dir, &entry, &closePx, &realized, &maxQty); err != nil {
		t.Fatal(err)
	}
	if n != 1 || dir != "long" || decimal.RequireFromString(entry).Cmp(decimal.NewFromInt(100000)) != 0 ||
		decimal.RequireFromString(closePx).Cmp(decimal.NewFromInt(101000)) != 0 || decimal.RequireFromString(realized).Cmp(decimal.NewFromInt(20)) != 0 ||
		decimal.RequireFromString(maxQty).Cmp(decimal.RequireFromString("0.02")) != 0 {
		t.Fatalf("history row: n=%d %s entry=%s close=%s pnl=%s max=%s", n, dir, entry, closePx, realized, maxQty)
	}
}
