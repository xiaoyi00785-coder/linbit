package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func seedBL(q string) Baseline { return resolveBaseline(Baseline{Asset: "SOL", SeedQty: d(q)}, true) }

// seeded 1M USDT of base, 0 net -> both sides quote (was: sell-only).
func TestSpotNetZeroBothSides(t *testing.T) {
	p := defPair() // max_position 50000
	inv := spotInventory(p, d("150"), d("6666.66666667"), d("10000000"), seedBL("6666.66666667"))
	if !inv.AllowBuy || !inv.AllowSell || !inv.NetQty.IsZero() || inv.BaselineSource != "seed" {
		t.Fatalf("%+v", inv)
	}
	_, qs := BuildLadder(p, btc, d("150"), inv)
	var b, s int
	for _, q := range qs {
		if q.Side == "buy" {
			b++
		} else {
			s++
		}
	}
	if b == 0 || s == 0 {
		t.Fatalf("buys=%d sells=%d", b, s)
	}
	// without the baseline the old behaviour (gross 1M >= 50k) would stop bids
	if spotInventory(p, d("150"), d("6666.66666667"), d("10000000"), Baseline{}).AllowBuy {
		t.Fatal("gross position should still be capped when no baseline")
	}
}

func TestSpotNetLongStopsBids(t *testing.T) {
	inv := spotInventory(defPair(), d("150"), d("7000"), d("10000000"), seedBL("6666")) // +334*150 = 50100
	if inv.AllowBuy || !inv.AllowSell || !inv.NetValue.Equal(d("50100")) {
		t.Fatalf("%+v", inv)
	}
}

func TestSpotNetShortStopsAsks(t *testing.T) {
	inv := spotInventory(defPair(), d("150"), d("6332"), d("10000000"), seedBL("6666")) // -334*150 = -50100
	if !inv.AllowBuy || inv.AllowSell || !inv.NetValue.Equal(d("-50100")) {
		t.Fatalf("%+v", inv)
	}
	_, qs := BuildLadder(defPair(), btc, d("150"), inv)
	for _, q := range qs {
		if q.Side == "sell" {
			t.Fatalf("ask while net short: %+v", q)
		}
	}
}

func TestSpotNetWithinCap(t *testing.T) {
	inv := spotInventory(defPair(), d("150"), d("6900"), d("10000000"), seedBL("6666")) // +35100
	if !inv.AllowBuy || !inv.AllowSell {
		t.Fatalf("%+v", inv)
	}
}

func TestResolveBaselinePrecedence(t *testing.T) {
	tb, sb := d("10"), d("3")
	cases := []struct {
		b       Baseline
		testnet bool
		qty     string
		src     string
	}{
		{Baseline{TargetBase: &tb, StartBase: &sb, SeedQty: d("5")}, true, "10", "admin"},
		{Baseline{StartBase: &sb, SeedQty: d("5")}, true, "5", "seed"},
		{Baseline{StartBase: &sb, SeedQty: d("5")}, false, "3", "start"}, // mainnet ignores seeds
		{Baseline{SeedQty: decimal.Zero}, true, "0", "none"},
	}
	for i, c := range cases {
		r := resolveBaseline(c.b, c.testnet)
		if !r.Qty.Equal(d(c.qty)) || r.Source != c.src {
			t.Fatalf("case %d: %s %s", i, r.Qty, r.Source)
		}
	}
}

func TestBaselinePatch(t *testing.T) {
	s := func(v string) *string { return &v }
	if _, err := (baselinePatch{TargetBase: s("-1")}).target(); err == nil {
		t.Fatal("negative accepted")
	}
	if v, err := (baselinePatch{TargetBase: s("12.5")}).target(); err != nil || !v.Equal(d("12.5")) {
		t.Fatal(v, err)
	}
	if v, _ := (baselinePatch{TargetBase: s("1"), ClearTarget: true}).target(); v != nil {
		t.Fatal("clear ignored")
	}
}
