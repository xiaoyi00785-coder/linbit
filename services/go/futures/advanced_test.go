package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestHedgeSide(t *testing.T) {
	cases := []struct {
		pos, side  string
		ro         bool
		want       string
		wantReduce bool
		err        bool
	}{
		{"LONG", "buy", false, "LONG", false, false},
		{"LONG", "sell", false, "LONG", true, false}, // closing forced reduce-only
		{"SHORT", "sell", false, "SHORT", false, false},
		{"SHORT", "buy", false, "SHORT", true, false},
		{"LONG", "buy", true, "", false, true}, // reduce-only opening rejected
		{"BOTH", "buy", false, "", false, true},
		{"", "buy", false, "", false, true},
	}
	for _, c := range cases {
		got, red, err := hedgeSide(c.pos, c.side, c.ro)
		if (err != nil) != c.err || got != c.want || red != c.wantReduce {
			t.Errorf("hedgeSide(%s,%s,%v) = %s,%v,%v", c.pos, c.side, c.ro, got, red, err)
		}
	}
}

func TestTriggerHelpers(t *testing.T) {
	d := decimal.RequireFromString
	if !fires("up", d("100"), d("100")) || fires("up", d("99.9"), d("100")) || !fires("down", d("99"), d("100")) || fires("down", d("101"), d("100")) {
		t.Fatal("fires")
	}
	for in, want := range map[string]string{"": "MARK", "last": "LAST", "MARK_PRICE": "MARK", "CONTRACT_PRICE": "LAST"} {
		if got, err := normTrigger(in, "MARK"); err != nil || got != want {
			t.Errorf("normTrigger(%q)=%s,%v", in, got, err)
		}
	}
	if _, err := normTrigger("INDEX", "MARK"); err == nil {
		t.Fatal("INDEX should be rejected")
	}
	if !losesPriority(d("1"), d("2"), d("1"), d("1")) || !losesPriority(d("1"), d("1"), d("1"), d("2")) || losesPriority(d("1"), d("1"), d("2"), d("1")) {
		t.Fatal("losesPriority")
	}
}

func TestOrderConfigValidate(t *testing.T) {
	c := defaultOrderConfig()
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	c.PlanDefaultTrigger = "INDEX"
	if c.validate() == nil {
		t.Fatal("bad trigger accepted")
	}
	c = defaultOrderConfig()
	c.MaxAlgoPerSymbol = 0
	if c.validate() == nil {
		t.Fatal("zero limit accepted")
	}
}
