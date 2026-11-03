package main

import (
	"testing"
	"time"
)

func TestParsePeriod(t *testing.T) {
	for p, want := range map[string]time.Duration{"5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour, "1d": 24 * time.Hour} {
		if got, ok := parsePeriod(p); !ok || got != want {
			t.Fatalf("%s = %v %v", p, got, ok)
		}
	}
	for _, p := range []string{"", "0m", "30s", "8d", "1w", "m5", "-1h"} {
		if _, ok := parsePeriod(p); ok {
			t.Fatalf("%q accepted", p)
		}
	}
}

func TestEvOutcome(t *testing.T) {
	cases := []struct {
		dir, open, settle, res, pay string
	}{
		{"up", "100", "101", "won", "18"},
		{"up", "100", "99", "lost", "0"},
		{"down", "100", "99", "won", "18"},
		{"down", "100", "101", "lost", "0"},
		{"up", "100", "100", "tie", "10"},
		{"down", "100", "100", "tie", "10"},
	}
	for _, c := range cases {
		r, p := evOutcome(c.dir, d(c.open), d(c.settle), d("10"), d("0.8"))
		if r != c.res || !p.Equal(d(c.pay)) {
			t.Fatalf("%+v => %s %s", c, r, p)
		}
	}
}

func TestExposureBreach(t *testing.T) {
	lim := d("50000")
	if exposureBreach(d("49000"), d("0"), d("1000"), "up", lim) {
		t.Fatal("exactly at limit must pass")
	}
	if !exposureBreach(d("49500"), d("0"), d("1000"), "up", lim) {
		t.Fatal("over limit must breach")
	}
	if exposureBreach(d("60000"), d("0"), d("1000"), "down", lim) {
		t.Fatal("reducing imbalance must pass even over limit")
	}
	if !exposureBreach(d("0"), d("49500"), d("1000"), "down", lim) {
		t.Fatal("down side breach")
	}
}

func TestEvConfigValidate(t *testing.T) {
	ok := evConfig{Periods: []string{"5m", "1h"}, PayoutRatio: d("0.8"), MinStake: d("5"), MaxStake: d("1000"), DailyLimit: d("10000"), ExposureLimit: d("50000"), SettleGraceSec: 120}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	bads := []func(c *evConfig){
		func(c *evConfig) { c.Periods = nil },
		func(c *evConfig) { c.Periods = []string{"5m", "5m"} },
		func(c *evConfig) { c.Periods = []string{"10s"} },
		func(c *evConfig) { c.PayoutRatio = d("0") },
		func(c *evConfig) { c.MinStake = d("2000") },
	}
	for i, f := range bads {
		c := ok
		c.Periods = append([]string(nil), ok.Periods...)
		f(&c)
		if c.validate() == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
