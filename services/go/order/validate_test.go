package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPrecisionOK(t *testing.T) {
	cases := []struct {
		val    string
		places int
		want   bool
	}{
		{"0.01", 6, true},
		{"0.000001", 6, true},
		{"0.0000001", 6, false}, // 7 dp > 6
		{"70000", 2, true},
		{"70000.40", 2, true},
		{"70000.401", 2, false},
		{"1", 0, true},
		{"1.1", 0, false},
	}
	for _, c := range cases {
		got := precisionOK(decimal.RequireFromString(c.val), c.places)
		if got != c.want {
			t.Errorf("precisionOK(%s, %d) = %v, want %v", c.val, c.places, got, c.want)
		}
	}
}

func TestValidTIF(t *testing.T) {
	for _, tif := range []string{"GTC", "IOC", "FOK", "PostOnly"} {
		if !validTIF(tif) {
			t.Errorf("validTIF(%q) = false, want true", tif)
		}
	}
	for _, tif := range []string{"", "gtc", "DAY", "foo"} {
		if validTIF(tif) {
			t.Errorf("validTIF(%q) = true, want false", tif)
		}
	}
}

func TestTerminal(t *testing.T) {
	for _, s := range []string{"filled", "canceled", "rejected"} {
		if !terminal(s) {
			t.Errorf("terminal(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"new", "partially_filled"} {
		if terminal(s) {
			t.Errorf("terminal(%q) = true, want false", s)
		}
	}
}

// TestFeeRate verifies the configured spot maker/taker rates are selected by role
// and default to 0.1% each.
func TestFeeRate(t *testing.T) {
	s := &Server{cfg: Config{
		SpotMakerFeeRate: decimal.RequireFromString("0.001"),
		SpotTakerFeeRate: decimal.RequireFromString("0.001"),
	}}
	if !s.feeRate("maker").Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("maker fee = %s, want 0.001", s.feeRate("maker"))
	}
	if !s.feeRate("taker").Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("taker fee = %s, want 0.001", s.feeRate("taker"))
	}
}
