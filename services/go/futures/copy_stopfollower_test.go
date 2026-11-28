package main

import "testing"

// TestCopyStopFollowerByTrader covers the restored trader-side "stop a
// follower" action: only the owning trader can stop, and it settles the follow.
func TestCopyStopFollowerByTrader(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1})
	nick := "Alpha"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	f := mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "10"})
	// a non-owning trader cannot stop this follow
	_, err := s.StopFollower(ctx, "mm", f.ID)
	mustCode(t, err, "FOLLOW_NOT_FOUND")
	// the owning trader stops the follower -> settled/refunded
	got, err := s.StopFollower(ctx, "trader", f.ID)
	if err != nil {
		t.Fatalf("stop follower: %v", err)
	}
	if got.Status != "stopped" && got.Status != "stopping" {
		t.Fatalf("status = %s, want stopped/stopping", got.Status)
	}
	// stopping again errors (already not active)
	if _, err := s.StopFollower(ctx, "trader", f.ID); err == nil {
		t.Fatal("expected error on second stop")
	}
}
