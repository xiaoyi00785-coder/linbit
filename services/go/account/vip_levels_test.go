package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

// pickTier is the pure-logic core of the hourly VIP recompute: it must pick
// the highest tier whose thresholds the user actually meets (volume OR assets
// if only one is set; both when both are set).
func TestPickTierVolumeOnly(t *testing.T) {
	tiers := []vipTier{
		{Level: 0, MinVolume30d: decimal.Zero, MinAssets: decimal.Zero},
		{Level: 1, MinVolume30d: decimal.NewFromInt(50000)},
		{Level: 2, MinVolume30d: decimal.NewFromInt(250000)},
		{Level: 3, MinVolume30d: decimal.NewFromInt(1000000)},
	}
	for _, c := range []struct {
		vol    int64
		assets int64
		want   int
	}{
		{0, 0, 0},
		{49999, 0, 0},
		{50000, 0, 1},
		{250000, 0, 2},
		{999999, 0, 2},
		{1000000, 0, 3},
		{9999999, 0, 3},
	} {
		got := pickTier(tiers, decimal.NewFromInt(c.vol), decimal.NewFromInt(c.assets)).Level
		if got != c.want {
			t.Errorf("vol=%d assets=%d: got level %d want %d", c.vol, c.assets, got, c.want)
		}
	}
}

func TestPickTierVolumeAndAssets(t *testing.T) {
	// A tier that requires both — a prop desk with no assets but massive
	// volume still gets it (volume OR assets semantics apply only when one of
	// the thresholds is zero; here both are set so both are required).
	tiers := []vipTier{
		{Level: 0, MinVolume30d: decimal.Zero, MinAssets: decimal.Zero},
		{Level: 1, MinVolume30d: decimal.NewFromInt(50000), MinAssets: decimal.NewFromInt(1000)},
	}
	if lv := pickTier(tiers, decimal.NewFromInt(100000), decimal.NewFromInt(500)).Level; lv != 0 {
		t.Errorf("volume but no assets: expected level 0, got %d", lv)
	}
	if lv := pickTier(tiers, decimal.NewFromInt(100000), decimal.NewFromInt(1000)).Level; lv != 1 {
		t.Errorf("both met: expected level 1, got %d", lv)
	}
}

func TestPickTierAssetsOnly(t *testing.T) {
	// A whale with large assets but zero volume reaches tiers whose
	// MinVolume30d is zero; higher tiers remain out of reach until the
	// volume threshold is met, matching the strict semantics we need to
	// prevent a cold wallet from unlocking VIP maker rates.
	tiers := []vipTier{
		{Level: 0, MinVolume30d: decimal.Zero, MinAssets: decimal.Zero},
		{Level: 1, MinAssets: decimal.NewFromInt(1000)},
		{Level: 2, MinVolume30d: decimal.NewFromInt(250000), MinAssets: decimal.NewFromInt(5000)},
	}
	if lv := pickTier(tiers, decimal.Zero, decimal.NewFromInt(5000)).Level; lv != 1 {
		t.Errorf("assets only (no volume): expected 1, got %d", lv)
	}
	if lv := pickTier(tiers, decimal.NewFromInt(250000), decimal.NewFromInt(5000)).Level; lv != 2 {
		t.Errorf("both met: expected 2, got %d", lv)
	}
}
