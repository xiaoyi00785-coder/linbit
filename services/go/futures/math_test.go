package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func eq(t *testing.T, name string, got, want decimal.Decimal) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s: got %s want %s", name, got, want)
	}
}

func TestApplyTradeOpenAddReduceFlip(t *testing.T) {
	q, e, pnl, cl := applyTrade(dZero, dZero, d("1"), d("100"))
	eq(t, "q", q, d("1"))
	eq(t, "e", e, d("100"))
	q, e, _, _ = applyTrade(q, e, d("1"), d("200"))
	eq(t, "avg", e, d("150"))
	q, e, pnl, cl = applyTrade(q, e, d("-0.5"), d("170"))
	eq(t, "pnl", pnl, d("10"))
	eq(t, "closed", cl, d("0.5"))
	eq(t, "entry kept", e, d("150"))
	q, e, pnl, cl = applyTrade(q, e, d("-2.5"), d("140"))
	eq(t, "flip pnl", pnl, d("-15"))
	eq(t, "flip closed", cl, d("1.5"))
	eq(t, "flip q", q, d("-1"))
	eq(t, "flip entry", e, d("140"))
	q, e, pnl, _ = applyTrade(q, e, d("1"), d("120"))
	eq(t, "short pnl", pnl, d("20"))
	eq(t, "flat", q, dZero)
	eq(t, "flat e", e, dZero)
}

func TestLiqPrice(t *testing.T) {
	// long 1 @ 50000, 20x isolated: margin 2500, mmr 0.4%
	p := liqPrice(d("1"), d("50000"), d("2500"), d("0.004"))
	// 2500 + (P-50000) = 0.004P  => P = 47500/0.996
	eq(t, "long", p.Round(4), d("47500").Div(d("0.996")).Round(4))
	// check identity at P
	lhs := d("2500").Add(p.Sub(d("50000")))
	if lhs.Sub(d("0.004").Mul(p)).Abs().GreaterThan(d("0.0000001")) {
		t.Fatalf("not at maintenance: %s", lhs)
	}
	s := liqPrice(d("-1"), d("50000"), d("2500"), d("0.004"))
	eq(t, "short", s.Round(4), d("52500").Div(d("1.004")).Round(4))
	eq(t, "over-collateralized long", liqPrice(d("1"), d("100"), d("200"), d("0.004")), dZero)
}

func TestFundingRate(t *testing.T) {
	I, c, cap := d("0.0001"), d("0.0005"), d("0.0075")
	eq(t, "flat premium -> interest", fundingRate(dZero, I, c, cap), d("0.0001"))
	eq(t, "small premium", fundingRate(d("0.0003"), I, c, cap), d("0.0001"))
	eq(t, "big premium", fundingRate(d("0.002"), I, c, cap), d("0.0015"))
	eq(t, "negative", fundingRate(d("-0.002"), I, c, cap), d("-0.0015"))
	eq(t, "capped", fundingRate(d("0.05"), I, c, cap), d("0.0075"))
	eq(t, "capped neg", fundingRate(d("-0.05"), I, c, cap), d("-0.0075"))
}

func TestMarkPrice(t *testing.T) {
	eq(t, "no last", markPrice(d("100"), dZero, dZero, d("0.5")), d("100"))
	eq(t, "last above", markPrice(d("100"), d("120"), dZero, d("0.5")), d("100"))
	eq(t, "basis between", markPrice(d("100"), d("120"), d("0.1"), d("0.5")), d("105"))
	eq(t, "last between", markPrice(d("100"), d("102"), d("0.1"), d("0.5")), d("102"))
	eq(t, "last below", markPrice(d("100"), d("90"), d("-0.1"), d("1")), d("90"))
}

func TestStepAndBucket(t *testing.T) {
	if !isStep(d("0.0003"), d("0.0001")) || isStep(d("0.00015"), d("0.0001")) || isStep(dZero, d("1")) {
		t.Fatal("isStep")
	}
	s := &Server{cfg: Config{FundingInterval: 8 * time.Hour}}
	at := time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)
	if got := s.nextFunding(at); !got.Equal(time.Date(2026, 11, 3, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("next funding %s", got)
	}
}

func TestTiersContinuous(t *testing.T) {
	c := Contract{MMR: d("0.004"), MaxLeverage: 200, Tiers: []Tier{
		{1, d("50000"), d("0.004"), d("0"), 200}, {2, d("500000"), d("0.005"), d("50"), 100}, {3, d("2000000"), d("0.01"), d("2550"), 50}}}
	for _, n := range []string{"50000", "500000"} {
		lo := c.maintMargin(d(n))
		hi := c.maintMargin(d(n).Add(d("0.00000001")))
		if hi.Sub(lo).Abs().GreaterThan(d("0.0001")) {
			t.Fatalf("discontinuous at %s: %s vs %s", n, lo, hi)
		}
	}
	if got := c.maintMargin(d("100000")); !got.Equal(d("450")) {
		t.Fatalf("mm=%s", got)
	}
	if got := c.maxNotional(150); !got.Equal(d("50000")) {
		t.Fatalf("maxN(150)=%s", got)
	}
	if got := c.maxNotional(50); !got.Equal(d("2000000")) {
		t.Fatalf("maxN(50)=%s", got)
	}
}

func TestADL(t *testing.T) {
	// long 1 @52000 bankrupt at mark 45000, uncovered 5956.6 -> closes at 50956.6
	if got := bankruptPrice(d("1"), d("45000"), d("5956.6")); !got.Equal(d("50956.6")) {
		t.Fatalf("px=%s", got)
	}
	if got := bankruptPrice(d("-2"), d("100"), d("10")); !got.Equal(d("95")) {
		t.Fatalf("px=%s", got)
	}
	a := adlScore(d("-1"), d("52000"), d("45000"), 20) // ROE 2.69 * 20
	b := adlScore(d("-1"), d("50000"), d("45000"), 20)
	l := adlScore(d("1"), d("40000"), d("45000"), 100) // losing short side n/a: long in profit
	if !a.GreaterThan(b) || !l.GreaterThan(a) {
		t.Fatalf("scores %s %s %s", a, b, l)
	}
	if s := adlScore(d("1"), d("50000"), d("45000"), 10); s.Sign() >= 0 {
		t.Fatalf("losing score %s", s)
	}
}
