package main

import "testing"

// TestCopyDealerIdentity covers the restored dealer_identity field: default,
// case-insensitive update, persistence and rejection of reserved values.
func TestCopyDealerIdentity(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1})
	nick := "Alpha"
	tr, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick})
	if err != nil {
		t.Fatal(err)
	}
	if tr.DealerIdentity != "PUBLIC_AREA" {
		t.Fatalf("default identity = %q, want PUBLIC_AREA", tr.DealerIdentity)
	}
	priv := "private_area" // case-insensitive, normalised to upper
	tr, err = s.UpdateTrader(ctx, "trader", traderReq{DealerIdentity: &priv})
	if err != nil || tr.DealerIdentity != "PRIVATE_AREA" {
		t.Fatalf("update identity: %v %q", err, tr.DealerIdentity)
	}
	got, err := s.getTrader(ctx, "trader")
	if err != nil || got.DealerIdentity != "PRIVATE_AREA" {
		t.Fatalf("getTrader identity = %q err=%v", got.DealerIdentity, err)
	}
	mech := "MECHANISM"
	_, err = s.UpdateTrader(ctx, "trader", traderReq{DealerIdentity: &mech})
	mustCode(t, err, "INVALID_DEALER_IDENTITY")
}

// TestCopyTraderClosedAt covers closed_at being set on close and cleared on
// re-activation.
func TestCopyTraderClosedAt(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1})
	nick := "Alpha"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.getTrader(ctx, "trader"); got.ClosedAt != nil {
		t.Fatalf("active trader closed_at = %v, want nil", got.ClosedAt)
	}
	if err := s.SetTraderStatus(ctx, "trader", "closed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.getTrader(ctx, "trader"); got.ClosedAt == nil {
		t.Fatal("closed trader closed_at = nil, want set")
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.getTrader(ctx, "trader"); got.ClosedAt != nil {
		t.Fatalf("re-activated trader closed_at = %v, want nil", got.ClosedAt)
	}
}

// TestCopyFollowLeverage covers the restored follow leverage field: persistence
// on create, range validation and updates (0 means follow the trader).
func TestCopyFollowLeverage(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1})
	nick := "Alpha"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	lev := 20
	f := mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "10", Leverage: &lev})
	if f.Leverage != 20 {
		t.Fatalf("follow leverage = %d, want 20", f.Leverage)
	}
	bad := 200
	_, err := s.UpdateFollow(ctx, "follower", f.ID, followReq{Leverage: &bad})
	mustCode(t, err, "INVALID_LEVERAGE")
	nl := 5
	if f2, err := s.UpdateFollow(ctx, "follower", f.ID, followReq{Leverage: &nl}); err != nil || f2.Leverage != 5 {
		t.Fatalf("update leverage: %v lev=%d", err, f2.Leverage)
	}
	zero := 0
	if f3, err := s.UpdateFollow(ctx, "follower", f.ID, followReq{Leverage: &zero}); err != nil || f3.Leverage != 0 {
		t.Fatalf("zero leverage: %v lev=%d", err, f3.Leverage)
	}
}
