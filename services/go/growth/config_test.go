package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestTierFor(t *testing.T) {
	c := Config{Tiers: parseTiers(defaultTiers)}
	for vol, want := range map[string]string{"0": "normal", "4999999.99": "normal", "5000000": "silver", "20000000": "gold", "60000000": "diamond"} {
		if got := c.tierFor(decimal.RequireFromString(vol)).Key; got != want {
			t.Fatalf("tierFor(%s)=%s want %s", vol, got, want)
		}
	}
	if r := c.tier("gold"); r.FuturesRate.String() != "0.45" || r.SpotRate.String() != "0.3" {
		t.Fatalf("gold rates %v", r)
	}
}

func TestLastBoundary(t *testing.T) {
	s := &Server{cfg: Config{SettleHourUTC: 16, DayOffset: 8 * time.Hour}}
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	if b := s.lastBoundary(now); !b.Equal(time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("boundary %v", b)
	}
	if d := s.day(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC)); d != "2026-11-03" {
		t.Fatalf("day %s", d)
	}
}
