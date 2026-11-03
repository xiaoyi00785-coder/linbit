package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestKYCLimitTiers(t *testing.T) {
	k := KYCLimitConfig{DailyUSDT: map[int]decimal.Decimal{
		0: decimal.NewFromInt(1000), 1: decimal.NewFromInt(50000), 2: decimal.NewFromInt(200000),
	}}
	for lvl, want := range map[int]int64{0: 1000, 1: 50000, 2: 200000, 5: 200000} {
		got, on := k.limit(lvl)
		if !on || !got.Equal(decimal.NewFromInt(want)) {
			t.Errorf("level %d: got %s on=%v, want %d", lvl, got, on, want)
		}
	}
	if _, on := (KYCLimitConfig{}).limit(0); on {
		t.Error("empty config should disable limits")
	}
}
