package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

// acceptance r8: fee bills rendered -0.0038384 vs -0.00383384; both must show 8 dp.
func TestFeeStrFixed8(t *testing.T) {
	for in, want := range map[string]string{
		"-0.0038384": "-0.00383840", "-0.00383384": "-0.00383384", "0": "0.00000000",
		"-0.000000001": "-0.000000001",
	} {
		if got := feeStr(decimal.RequireFromString(in)); got != want {
			t.Errorf("feeStr(%s)=%s want %s", in, got, want)
		}
	}
}

// Realized PnL showed -0.00911 next to -0.0354; positions history and the PnL
// summary now share the fee rendering (fixed 8 dp, exact if longer).
func TestPnlStrFixed8(t *testing.T) {
	for in, want := range map[string]string{"-0.00911": "-0.00911000", "-0.0354": "-0.03540000", "12.5": "12.50000000"} {
		if got := feeStr(money(decimal.RequireFromString(in))); got != want {
			t.Errorf("pnl %s=%s want %s", in, got, want)
		}
	}
}
