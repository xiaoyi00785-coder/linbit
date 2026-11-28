package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestApplyBookMirrorOrderFlipsPreMirrorSide(t *testing.T) {
	t0 := time.Now()
	mirrors := map[string]PositionMirror{
		mirrorKey("BTCUSDT", "BOTH"): {
			Symbol: "BTCUSDT", PositionSide: "BOTH", Status: "active", BookFlipped: true,
			CreatedAt: t0,
		},
	}
	o := Order{Symbol: "BTCUSDT", Side: "buy", PosSide: "BOTH", CreatedAt: t0.Add(-time.Minute)}
	applyBookMirrorOrder(&o, mirrors)
	if o.Side != "sell" {
		t.Fatalf("got side %s", o.Side)
	}
	o2 := Order{Symbol: "BTCUSDT", Side: "buy", PosSide: "BOTH", CreatedAt: t0.Add(time.Minute)}
	applyBookMirrorOrder(&o2, mirrors)
	if o2.Side != "buy" {
		t.Fatalf("post-mirror order should stay buy, got %s", o2.Side)
	}
}

func TestTargetSubsidy(t *testing.T) {
	u := decimal.RequireFromString("-100")
	if got := targetSubsidy(u); !got.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("got %s", got)
	}
	u = decimal.RequireFromString("50")
	if got := targetSubsidy(u); !got.Equal(decimal.RequireFromString("-100")) {
		t.Fatalf("profit side got %s", got)
	}
}

func TestApplyUserViewFlipsPnl(t *testing.T) {
	st := AcctState{
		Balance:   decimal.RequireFromString("1000"),
		CrossUPnL: decimal.RequireFromString("-100"),
		Equity:    decimal.RequireFromString("900"),
		Available: decimal.RequireFromString("800"),
		Positions: []PosView{{
			Symbol:  "BTCUSDT",
			PosSide: "BOTH",
			Side:    "long",
			UPnL:    "-100",
			ROE:     "-0.1",
			Mode:    "cross",
			upnl:    decimal.RequireFromString("-100"),
		}},
	}
	mirrors := map[string]PositionMirror{mirrorKey("BTCUSDT", "BOTH"): {Status: "active"}}
	applyUserView(&st, mirrors)
	if st.Positions[0].Side != "short" || st.Positions[0].UPnL != "100" {
		t.Fatalf("pos view %+v", st.Positions[0])
	}
	if !st.CrossUPnL.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("cross %s", st.CrossUPnL)
	}
}

func TestApplyUserViewBookFlippedNoDoubleFlip(t *testing.T) {
	st := AcctState{
		Balance:   decimal.RequireFromString("1000"),
		CrossUPnL: decimal.RequireFromString("100"),
		Equity:    decimal.RequireFromString("1100"),
		Positions: []PosView{{
			Symbol: "BTCUSDT", PosSide: "BOTH", Side: "short", UPnL: "100", Mode: "cross",
			upnl: decimal.RequireFromString("100"),
		}},
	}
	mirrors := map[string]PositionMirror{mirrorKey("BTCUSDT", "BOTH"): {BookFlipped: true, Status: "active"}}
	applyUserView(&st, mirrors)
	if !st.Positions[0].Mirrored || st.Positions[0].Side != "short" || st.Positions[0].UPnL != "100" {
		t.Fatalf("book flip should not display-flip: %+v", st.Positions[0])
	}
	if !st.CrossUPnL.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("cross %s", st.CrossUPnL)
	}
}

func TestCrossUPnLUserViewAndLiqEquity(t *testing.T) {
	st := AcctState{
		Balance:   decimal.RequireFromString("500"),
		CrossUPnL: decimal.RequireFromString("200"),
		CrossMM:   decimal.RequireFromString("100"),
		Positions: []PosView{{
			Symbol: "BTCUSDT", PosSide: "BOTH", Mode: "cross",
			upnl: decimal.RequireFromString("200"),
		}},
	}
	m := map[string]PositionMirror{mirrorKey("BTCUSDT", "BOTH"): {Status: "active"}}
	uv := crossUPnLUserView(st, m)
	if !uv.Equal(decimal.RequireFromString("-200")) {
		t.Fatalf("user cross upnl %s", uv)
	}
	subsidy := decimal.RequireFromString("300")
	lb := st.Balance.Sub(subsidy)
	userCross := lb.Add(uv)
	if !userCross.LessThanOrEqual(st.CrossMM) {
		t.Fatalf("expected user-view cross liq, got %s vs mm %s", userCross, st.CrossMM)
	}
}