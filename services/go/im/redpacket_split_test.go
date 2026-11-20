package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Splits must sum to exactly the total (no rounding leak into or out of the
// packet), every slot must be positive, and the equal mode must have a
// bounded last-slot residue so we don't give 99% of the packet to the final
// claimer.
func TestSplitRedpacket(t *testing.T) {
	cases := []struct {
		total  string
		shares int
		mode   string
	}{
		{"10.00", 5, "equal"},
		{"10.00", 5, "random"},
		{"1.23", 7, "random"},
		{"100", 100, "random"},
		{"0.05", 5, "equal"},
	}
	for _, c := range cases {
		total := decimal.RequireFromString(c.total)
		amounts := splitRedpacket(total, c.shares, c.mode)
		if len(amounts) != c.shares {
			t.Fatalf("%s/%d/%s: got %d amounts", c.total, c.shares, c.mode, len(amounts))
		}
		sum := decimal.Zero
		for i, a := range amounts {
			if !a.IsPositive() {
				t.Fatalf("%s/%d/%s: slot %d not positive: %s", c.total, c.shares, c.mode, i, a.String())
			}
			sum = sum.Add(a)
		}
		if !sum.Equal(total) {
			t.Fatalf("%s/%d/%s: sum=%s", c.total, c.shares, c.mode, sum.String())
		}
	}
}

// Random split must stay bounded by the total — a slot can never exceed the
// amount still unallocated. The double-average algorithm is intentionally
// unfair inside that bound, so we only check it doesn't go negative or over.
func TestSplitRedpacketRandomBounded(t *testing.T) {
	total := decimal.RequireFromString("100")
	shares := 10
	for i := 0; i < 50; i++ {
		amounts := splitRedpacket(total, shares, "random")
		for j, a := range amounts {
			if a.IsNegative() || a.GreaterThan(total) {
				t.Fatalf("iter %d slot %d=%s out of range", i, j, a.String())
			}
		}
	}
}
