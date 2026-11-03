package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestOverrideDefaultDepth(t *testing.T) {
	t.Setenv("GROWTH_PARTNER_OVERRIDE_DEPTH", "")
	t.Setenv("DATABASE_URL", "postgres://test@localhost/test")
	if d := LoadConfig().OverrideDepth; d != 2 {
		t.Fatalf("default depth %d want 2", d)
	}
}

// P1 <- P2 <- P3 <- P4 (P4 is the direct partner); all active.
func TestOverrideTwoLevels(t *testing.T) {
	parents := map[string]string{"P4": "P3", "P3": "P2", "P2": "P1"}
	tiers := map[string]string{"P1": "normal", "P2": "normal", "P3": "silver", "P4": "normal"}
	hops := overrideChain(parents, tiers, "P4", 2)
	if len(hops) != 2 || hops[0] != (overrideHop{"P3", "P4"}) || hops[1] != (overrideHop{"P2", "P3"}) {
		t.Fatalf("hops %v", hops)
	}
	for _, h := range hops {
		if h.Ben == "P1" {
			t.Fatal("level 3 must get nothing")
		}
	}
	// amounts: fee base 100 futures, direct normal 30% = 30; each override 10% of direct = 3.
	direct := decimal.NewFromInt(100).Mul(decimal.RequireFromString("0.30"))
	ov := direct.Mul(decimal.RequireFromString("0.10"))
	if direct.String() != "30" || ov.String() != "3" {
		t.Fatalf("direct %s ov %s", direct, ov)
	}
	// inactive level-2 stops the chain
	delete(tiers, "P2")
	if h := overrideChain(parents, tiers, "P4", 2); len(h) != 1 {
		t.Fatalf("inactive stop %v", h)
	}
	// cycle safety
	if h := overrideChain(map[string]string{"A": "B", "B": "A"}, map[string]string{"A": "x", "B": "x"}, "A", 5); len(h) != 1 {
		t.Fatalf("cycle %v", h)
	}
}
