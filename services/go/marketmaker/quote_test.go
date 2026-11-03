package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func defPair() Pair {
	return Pair{Market: "spot", Symbol: "BTCUSDT", Enabled: true, SpreadBps: d("10"), Levels: 5, LevelStepBps: d("5"),
		LevelNotional: d("1000"), SizeGrowth: d("1.2"), RequoteBps: d("2"), TargetBaseRatio: d("0.5"),
		MaxPosition: d("50000"), MaxSkewBps: d("20"), Leverage: 5}
}

var btc = Rules{Market: "spot", Symbol: "BTCUSDT", Base: "BTC", Quote: "USDT", Tick: d("0.01"), Step: d("0.00001"), MinQty: d("0.00001"), MinNotional: d("5")}

func TestLadderSymmetric(t *testing.T) {
	inv := Inventory{AllowBuy: true, AllowSell: true}
	mid, qs := BuildLadder(defPair(), btc, d("100000"), inv)
	if !mid.Equal(d("100000")) || len(qs) != 10 {
		t.Fatalf("mid=%s n=%d", mid, len(qs))
	}
	if !qs[0].Price.Equal(d("99950")) || !qs[1].Price.Equal(d("100050")) {
		t.Fatalf("level0 %s/%s", qs[0].Price, qs[1].Price)
	}
	// spread 0.1%
	if s := qs[1].Price.Sub(qs[0].Price).Div(d("100000")); !s.Equal(d("0.001")) {
		t.Fatalf("spread %s", s)
	}
	if !qs[2].Price.Equal(d("99900")) || !qs[9].Price.Equal(d("100250")) {
		t.Fatalf("levels %s %s", qs[2].Price, qs[9].Price)
	}
	if !qs[0].Qty.Equal(d("0.01")) || !qs[2].Qty.GreaterThan(qs[0].Qty) {
		t.Fatalf("qty %s %s", qs[0].Qty, qs[2].Qty)
	}
}

func TestInventorySkew(t *testing.T) {
	p := defPair()
	// all base, no quote -> fully long: mid shifts down, no buys
	inv := spotInventory(p, d("100000"), d("1"), d("0"), Baseline{})
	if !inv.Imbalance.Equal(d("1")) || inv.AllowBuy || !inv.AllowSell {
		t.Fatalf("%+v", inv)
	}
	mid, qs := BuildLadder(p, btc, d("100000"), inv)
	if !mid.Equal(d("99800")) {
		t.Fatalf("mid %s", mid)
	}
	for _, q := range qs {
		if q.Side != "sell" || q.Price.LessThanOrEqual(d("100000")) {
			t.Fatalf("bad quote %+v", q)
		}
	}
	// balanced
	inv = spotInventory(p, d("100000"), d("0.1"), d("10000"), Baseline{})
	if !inv.Imbalance.IsZero() || !inv.AllowBuy || !inv.AllowSell {
		t.Fatalf("%+v", inv)
	}
}

func TestFuturesCap(t *testing.T) {
	p := defPair()
	inv := futuresInventory(p, d("100000"), d("0.6")) // 60k long > 50k cap
	if inv.AllowBuy || !inv.AllowSell || !inv.Imbalance.Equal(d("1")) {
		t.Fatalf("%+v", inv)
	}
	inv = futuresInventory(p, d("100000"), d("-0.25"))
	if !inv.AllowBuy || !inv.AllowSell || !inv.Imbalance.Equal(d("-0.5")) {
		t.Fatalf("%+v", inv)
	}
}

func TestDiff(t *testing.T) {
	targets := []Quote{{Side: "buy", Price: d("99950"), Qty: d("0.01")}, {Side: "sell", Price: d("100050"), Qty: d("0.01")}}
	resting := []restingOrder{
		{ID: "a", Side: "buy", Price: d("99960"), Remaining: d("0.01")},   // 1bp off -> keep
		{ID: "b", Side: "sell", Price: d("100200"), Remaining: d("0.01")}, // 15bp off -> cancel
		{ID: "c", Side: "buy", Price: d("99950"), Remaining: d("0.01")},   // duplicate -> cancel
	}
	c, p := Diff(resting, targets, d("2"))
	if len(c) != 2 || c[0].ID != "b" || c[1].ID != "c" || len(p) != 1 || p[0].Side != "sell" {
		t.Fatalf("cancel=%+v place=%+v", c, p)
	}
}

func TestPatchValidate(t *testing.T) {
	bad := d("0.1")
	if (pairPatch{SpreadBps: &bad}).Validate() == nil {
		t.Fatal("spread too small accepted")
	}
	lv := 50
	if (pairPatch{Levels: &lv}).Validate() == nil {
		t.Fatal("levels accepted")
	}
	if (pairPatch{}).Validate() != nil {
		t.Fatal("empty patch rejected")
	}
}

func TestSeedQty(t *testing.T) {
	for _, c := range []struct{ v, p, want string }{
		{"1000000", "100000", "10"},
		{"1000000", "3000", "333.33333333"},
		{"1000000", "0.12345", "8100445.52450384"},
	} {
		if got := SeedQty(decimal.RequireFromString(c.v), decimal.RequireFromString(c.p)); got.String() != c.want {
			t.Fatalf("SeedQty(%s,%s)=%s want %s", c.v, c.p, got, c.want)
		}
	}
}
