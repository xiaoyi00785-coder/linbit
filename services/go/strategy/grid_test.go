package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestArithmeticLevels(t *testing.T) {
	lv, err := Levels("arithmetic", d("100"), d("200"), 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"100", "125", "150", "175", "200"}
	for i, w := range want {
		if !lv[i].Equal(d(w)) {
			t.Fatalf("level %d = %s want %s", i, lv[i], w)
		}
	}
}

func TestGeometricLevels(t *testing.T) {
	lv, err := Levels("geometric", d("100"), d("400"), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !lv[1].Equal(d("200")) {
		t.Fatalf("mid = %s", lv[1])
	}
	if _, err := Levels("arithmetic", d("1"), d("1.01"), 50, 2); err == nil {
		t.Fatal("expected collapse error")
	}
}

func TestPlan(t *testing.T) {
	lv, _ := Levels("arithmetic", d("100"), d("200"), 4, 2) // 100 125 150 175 200
	pl := MakePlan(lv, d("160"), d("1000"), d("0"), d("0"), 4)
	// buys at 100,125,150 ; sells at 175,200 ; q = 1000/(375+2*160)
	if len(pl.BuyLevels) != 3 || len(pl.SellLevels) != 2 {
		t.Fatalf("plan %+v", pl)
	}
	if !pl.Qty.Equal(d("1.4388")) {
		t.Fatalf("qty %s", pl.Qty)
	}
	if lo, hi := GridProfitRange(lv, d("0")); !lo.Round(4).Equal(d("0.1429")) || !hi.Equal(d("0.25")) {
		t.Fatalf("range %s %s", lo, hi)
	}
}
