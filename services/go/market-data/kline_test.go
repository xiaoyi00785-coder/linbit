package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestBucketStartAlignment(t *testing.T) {
	cases := []struct {
		interval string
		t        int64
	}{
		{"1m", 1762070400},
		{"1m", 1762070459},
		{"5m", 1762070401},
		{"15m", 1762070999},
		{"1h", 1762073999},
		{"1d", 1762070400},
		{"1w", 1762070400},
	}
	for _, c := range cases {
		iv, ok := LookupInterval(c.interval)
		if !ok {
			t.Fatalf("unknown interval %s", c.interval)
		}
		b := iv.bucketStart(c.t)
		if b > c.t || c.t-b >= iv.Seconds {
			t.Fatalf("%s: bucket %d not within [t-%d, t] of %d", c.interval, b, iv.Seconds, c.t)
		}
		if (b-iv.anchor)%iv.Seconds != 0 {
			t.Fatalf("%s: bucket %d not aligned to anchor %d step %d", c.interval, b, iv.anchor, iv.Seconds)
		}
	}
}

func TestWeekBucketMondayAligned(t *testing.T) {
	iv, _ := LookupInterval("1w")
	// mondayAnchor is a Monday 00:00 UTC; a timestamp inside that week must map
	// back to it, and one week later must advance by exactly 604800s.
	if got := iv.bucketStart(mondayAnchor + 100); got != mondayAnchor {
		t.Fatalf("week bucket = %d, want %d", got, mondayAnchor)
	}
	if got := iv.bucketStart(mondayAnchor + 604800 + 50); got != mondayAnchor+604800 {
		t.Fatalf("next week bucket = %d, want %d", got, mondayAnchor+604800)
	}
}

func TestCandleAdd(t *testing.T) {
	c := newCandle(100, dec("70000"), dec("0.01"))
	c.add(dec("70500"), dec("0.02"))
	c.add(dec("69000"), dec("0.01"))
	if c.Open.String() != "70000" {
		t.Fatalf("open = %s", c.Open)
	}
	if c.High.String() != "70500" {
		t.Fatalf("high = %s", c.High)
	}
	if c.Low.String() != "69000" {
		t.Fatalf("low = %s", c.Low)
	}
	if c.Close.String() != "69000" {
		t.Fatalf("close = %s", c.Close)
	}
	if c.Volume.String() != "0.04" {
		t.Fatalf("volume = %s", c.Volume)
	}
	if c.Trades != 3 {
		t.Fatalf("trades = %d", c.Trades)
	}
}
