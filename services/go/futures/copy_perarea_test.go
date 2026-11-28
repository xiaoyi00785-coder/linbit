package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestCopyPerAreaFollowerLimit(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1, "mm": 1})
	s.cfg.CopyMaxFollowersPublic = 50
	s.cfg.CopyMaxFollowersPrivate = 1
	nick, inv, priv := "Priv", "PRIV01", "PRIVATE_AREA"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, InvitationCode: &inv, DealerIdentity: &priv}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	if got := s.maxFollowersFor("PRIVATE_AREA"); got != 1 {
		t.Fatalf("private cap = %d, want 1", got)
	}
	if got := s.maxFollowersFor("PUBLIC_AREA"); got != 50 {
		t.Fatalf("public cap = %d, want 50", got)
	}
	mustFollow(t, s, ctx, "follower", followReq{TraderID: "trader", Amount: "200", InvitationCode: inv, Mode: "ratio", Ratio: "10", SupportSymbols: "BTCUSDT"})
	_, err := s.Follow(ctx, "mm", followReq{TraderID: "trader", Amount: "200", InvitationCode: inv, Mode: "ratio", Ratio: "10", SupportSymbols: "BTCUSDT"})
	mustCode(t, err, "TRADER_FULL")
}

func TestCopyPerAreaShareCap(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1})
	s.cfg.CopyMaxSharePublic = decimal.RequireFromString("0.3")
	s.cfg.CopyMaxSharePrivate = decimal.RequireFromString("0.5")
	nick, pub, over := "Alpha", "PUBLIC_AREA", "0.45"
	_, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, DealerIdentity: &pub, ProfitShare: &over})
	mustCode(t, err, "INVALID_PROFIT_SHARE")
	priv, inv, ok := "PRIVATE_AREA", "PRIV01", "0.45"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, DealerIdentity: &priv, InvitationCode: &inv, ProfitShare: &ok}); err != nil {
		t.Fatalf("private 0.45 should pass: %v", err)
	}
}

func TestCopyPerAreaShareCapOnUpdate(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1})
	s.cfg.CopyMaxSharePublic = decimal.RequireFromString("0.3")
	s.cfg.CopyMaxSharePrivate = decimal.RequireFromString("0.5")
	nick, priv, inv, ok := "Alpha", "PRIVATE_AREA", "PRIV01", "0.45"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, DealerIdentity: &priv, InvitationCode: &inv, ProfitShare: &ok}); err != nil {
		t.Fatal(err)
	}
	// switching to PUBLIC while keeping 0.45 share must be rejected (public cap 0.30)
	pub := "PUBLIC_AREA"
	_, err := s.UpdateTrader(ctx, "trader", traderReq{DealerIdentity: &pub})
	mustCode(t, err, "INVALID_PROFIT_SHARE")
	// raising share to 0.45 on a public trader must also be rejected
	s2, ctx2 := copyServer(t, map[string]int{"trader": 1})
	s2.cfg.CopyMaxSharePublic = decimal.RequireFromString("0.3")
	nick2, over := "Beta", "0.45"
	if _, err := s2.ApplyTrader(ctx2, "trader", traderReq{Nickname: &nick2}); err != nil {
		t.Fatal(err)
	}
	_, err = s2.UpdateTrader(ctx2, "trader", traderReq{ProfitShare: &over})
	mustCode(t, err, "INVALID_PROFIT_SHARE")
}
