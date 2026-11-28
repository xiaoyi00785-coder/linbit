package main

import "testing"

func TestCopyBlacklist(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1})
	nick := "Alpha"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	f := mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "10", SupportSymbols: "BTCUSDT"})
	if err := s.AddBlacklist(ctx, "trader", "follower", "spam"); err != nil {
		t.Fatalf("add blacklist: %v", err)
	}
	fs, err := s.follows(ctx, `id=$1`, f.ID)
	if err != nil || len(fs) == 0 || fs[0].Status != "stopped" {
		t.Fatalf("live follow not stopped on blacklist: %v %+v", err, fs)
	}
	_, err = s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "10", SupportSymbols: "BTCUSDT"})
	mustCode(t, err, "BLACKLISTED")
	list, err := s.listBlacklist(ctx, "trader")
	if err != nil || len(list) != 1 || list[0]["userId"] != "follower" {
		t.Fatalf("list: %v %+v", err, list)
	}
	mustCode(t, s.AddBlacklist(ctx, "trader", "trader", ""), "CANNOT_BLACKLIST_SELF")
	if err := s.RemoveBlacklist(ctx, "trader", "follower"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Follow(ctx, "follower", followReq{TraderID: "trader", Amount: "200", Mode: "ratio", Ratio: "10", SupportSymbols: "BTCUSDT"}); err != nil {
		t.Fatalf("follow after unblacklist: %v", err)
	}
}
