package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

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
	mirrors := map[string]PositionMirror{mirrorKey("BTCUSDT", "BOTH"): {}}
	applyUserView(&st, mirrors)
	if st.Positions[0].Side != "short" || st.Positions[0].UPnL != "100" {
		t.Fatalf("pos view %+v", st.Positions[0])
	}
	if !st.CrossUPnL.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("cross %s", st.CrossUPnL)
	}
}