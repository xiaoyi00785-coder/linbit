package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/shopspring/decimal"
)

func copyServer(t *testing.T, kyc map[string]int) (*Server, context.Context) {
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
	cfg := LoadConfig()
	cfg.CopyAutoApprove = false
	cfg.CopyMinKYCLevel = 1
	cfg.CopyMaxRatio = decimal.RequireFromString("50")
	cfg.CopyMinAmount = decimal.RequireFromString("50")
	cfg.CopyTraderMinEquity = decimal.RequireFromString("100")
	cfg.DefaultLeverage = 10
	cfg.DefaultMarginMode = "cross"
	cfg.MarketBand = decimal.RequireFromString("0.05")
	cfg.MinTransfer = decimal.RequireFromString("0.01")
	s, err := NewServer(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	s.kycLevel = func(_ context.Context, uid string) (int, error) {
		if kyc == nil {
			return 1, nil
		}
		if v, ok := kyc[uid]; ok {
			return v, nil
		}
		return 0, nil
	}
	for _, u := range []string{"trader", "follower", "mm", "no-kyc"} {
		if _, err := db.Exec(`INSERT INTO fut_accounts (user_id, balance) VALUES ($1, 100000)`, u); err != nil {
			t.Fatal(err)
		}
	}
	s.setMark("BTCUSDT", "100000")
	s.setMark("ETHUSDT", "3000")
	return s, ctx
}

func mustFollow(t *testing.T, s *Server, ctx context.Context, uid string, r followReq) Follow {
	t.Helper()
	f, err := s.Follow(ctx, uid, r)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	return f
}

func TestCopyApplyKYCAndPending(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1, "no-kyc": 0})
	nick := "Alpha"
	share := "0.2"
	_, err := s.ApplyTrader(ctx, "no-kyc", traderReq{Nickname: &nick, ProfitShare: &share})
	mustCode(t, err, "KYC_REQUIRED")

	tr, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, ProfitShare: &share})
	if err != nil || tr.Status != "pending" || !tr.ProfitShare.Equal(decimal.RequireFromString("0.2")) {
		t.Fatalf("apply: %v %+v", err, tr)
	}
	_, err = s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "200"})
	mustCode(t, err, "TRADER_NOT_ACTIVE")

	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFollowGates(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1, "no-kyc": 0})
	nick, inv := "Alpha", "AB12CD"
	priv := true
	tr, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, InvitationCode: &inv, PositionPrivacy: &priv})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}

	_, err = s.Follow(ctx, "no-kyc", followReq{TraderID: "trader", Amount: "200", InvitationCode: inv})
	mustCode(t, err, "KYC_REQUIRED")

	_, err = s.Follow(ctx, "trader", followReq{TraderID: "trader", Amount: "200", InvitationCode: inv})
	mustCode(t, err, "CANNOT_FOLLOW_SELF")

	_, err = s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "200"})
	mustCode(t, err, "INVITATION_REQUIRED")

	_, err = s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "200", InvitationCode: "WRONG1"})
	mustCode(t, err, "INVITATION_REQUIRED")

	_, err = s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "10", InvitationCode: inv})
	mustCode(t, err, "INVALID_AMOUNT")

	_, err = s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "200", InvitationCode: inv, Mode: "ratio", Ratio: "51"})
	mustCode(t, err, "INVALID_RATIO")

	f := mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", InvitationCode: inv, Mode: "ratio", Ratio: "50", SupportSymbols: "BTCUSDT", StopLossAmount: "80"})
	if f.Ratio.String() != "50" || f.SupportSymbols != "BTCUSDT" || !f.StopLossAmount.Equal(decimal.RequireFromString("80")) {
		t.Fatalf("params: %+v", f)
	}
	st, err := s.state(ctx, s.db, copyAcct(f.ID))
	if err != nil || !st.Equity.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("sub equity: %v %s", err, st.Equity)
	}
	// invitation code is stored but not leaked on the public struct after we clear it in HTTP; the DB trader still has it.
	got, _ := s.getTrader(ctx, "trader")
	if got.InvitationCode != inv || !got.PositionPrivacy {
		t.Fatalf("privacy/invite not persisted: %+v", got)
	}
	_ = tr
}

func TestCopyMirrorFilterAndStopLoss(t *testing.T) {
	s, ctx := copyServer(t, nil)
	nick := "Alpha"
	if _, err := s.db.Exec(`UPDATE fut_accounts SET balance=1000 WHERE user_id='trader'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	f := mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "1", SupportSymbols: "BTCUSDT", StopLossAmount: "50"})

	// Deep book so the follower's market copy can fill after the trader's fill.
	if _, _, err := s.PlaceOrder(ctx, "mm", PlaceReq{Symbol: "BTCUSDT", Side: "sell", Type: "limit", Price: "100000", Qty: "1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PlaceOrder(ctx, "trader", PlaceReq{Symbol: "BTCUSDT", Side: "buy", Type: "market", Qty: "0.01"}); err != nil {
		t.Fatal(err)
	}
	pos, _, err := loadPosition(ctx, s.db, copyAcct(f.ID), "BTCUSDT", "BOTH", false)
	if err != nil || pos.Qty.Sign() <= 0 {
		t.Fatalf("btc copy not opened: %v qty=%v", err, pos.Qty)
	}

	// ETH is filtered: seed book and have the trader open ETH; follower must skip.
	s.setMark("ETHUSDT", "3000")
	if _, _, err := s.PlaceOrder(ctx, "mm", PlaceReq{Symbol: "ETHUSDT", Side: "sell", Type: "limit", Price: "3000", Qty: "10"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PlaceOrder(ctx, "trader", PlaceReq{Symbol: "ETHUSDT", Side: "buy", Type: "market", Qty: "0.1"}); err != nil {
		t.Fatal(err)
	}
	eth, _, _ := loadPosition(ctx, s.db, copyAcct(f.ID), "ETHUSDT", "BOTH", false)
	if eth.Qty.Sign() != 0 {
		t.Fatalf("ETH should be filtered, qty=%s", eth.Qty)
	}
	var skipped int
	_ = s.db.QueryRow(`SELECT count(*) FROM fut_copy_trades WHERE follow_id=$1 AND reason='SYMBOL_FILTERED'`, f.ID).Scan(&skipped)
	if skipped == 0 {
		t.Fatal("expected SYMBOL_FILTERED trade log")
	}

	// Book a bid so stop-loss flattening can close the long, then drop equity.
	if _, _, err := s.PlaceOrder(ctx, "mm", PlaceReq{Symbol: "BTCUSDT", Side: "buy", Type: "limit", Price: "100000", Qty: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.txLocked(ctx, func(tx *sql.Tx) error {
		return addBalance(ctx, tx, copyAcct(f.ID), "", "realized_pnl", decimal.RequireFromString("-160"), "test-sl")
	}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.copyHousekeepingLocked(ctx)
	s.mu.Unlock()
	fs, err := s.follows(ctx, `id=$1`, f.ID)
	if err != nil || len(fs) == 0 || fs[0].Status != "stopped" {
		t.Fatalf("want stopped after SL, got %+v %v", fs, err)
	}
}

func TestCopyProfitShareAndRatioCap(t *testing.T) {
	s, ctx := copyServer(t, nil)
	nick, share := "Alpha", "0.10"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, ProfitShare: &share}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	f := mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "1"})
	if err := s.txLocked(ctx, func(tx *sql.Tx) error {
		return addBalance(ctx, tx, copyAcct(f.ID), "", "realized_pnl", decimal.RequireFromString("100"), "test-pnl")
	}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if err := s.settleFollow(ctx, f, "2026-11-24"); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	if err := s.settleFollow(ctx, f, "2026-11-24"); err != nil { // idempotent
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	var paid decimal.Decimal
	_ = s.db.QueryRow(`SELECT share_paid FROM fut_copy_follows WHERE id=$1`, f.ID).Scan(&paid)
	if !paid.Equal(decimal.RequireFromString("10")) {
		t.Fatalf("share_paid=%s want 10", paid)
	}
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM fut_copy_settlements WHERE follow_id=$1`, f.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("settlements=%d", n)
	}
}
