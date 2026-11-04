package main

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func vc() Contract {
	return Contract{Symbol: "BTCUSDT", MaxLeverage: 200, Tick: decimal.RequireFromString("0.1"),
		MinQty: decimal.RequireFromString("0.001"), MaxQty: decimal.RequireFromString("100")}
}

func code(err error) string {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestCheckQty(t *testing.T) {
	c := vc()
	for raw, want := range map[string]string{"0.001": "", "1.234": "", "100": "", "0.0005": "INVALID_QTY", "0.0001": "INVALID_QTY",
		"0.0015": "INVALID_QTY", "100.001": "INVALID_QTY", "0": "INVALID_QTY", "-1": "INVALID_QTY", "abc": "INVALID_QTY"} {
		if _, err := checkQty(c, raw); code(err) != want {
			t.Errorf("qty %s: got %q want %q (%v)", raw, code(err), want, err)
		}
	}
}

func TestCheckPriceAndTPSL(t *testing.T) {
	c := vc()
	for raw, want := range map[string]string{"65000.1": "", "65000.15": "INVALID_PRICE", "65000.123456": "INVALID_PRICE", "0": "INVALID_PRICE", "x": "INVALID_PRICE"} {
		if _, err := checkPrice(c, raw, "价格"); code(err) != want {
			t.Errorf("price %s: got %q want %q", raw, code(err), want)
		}
	}
	if v, err := checkOptPrice(c, "", "止盈价"); err != nil || v.Valid {
		t.Fatal("empty TP must be unset")
	}
	if _, err := checkOptPrice(c, "70000.05", "止盈价"); code(err) != "INVALID_PRICE" {
		t.Fatal("off-tick TP must be rejected")
	}
	if v, err := checkOptPrice(c, "70000.5", "止损价"); err != nil || !v.Valid {
		t.Fatal("on-tick SL must pass")
	}
}

func TestCheckLeverage(t *testing.T) {
	c := vc()
	for lev, ok := range map[int]bool{1: true, 125: true, 200: true, 0: false, 201: false, 500: false, -5: false} {
		if err := checkLeverage(c, lev); (err == nil) != ok {
			t.Errorf("lev %d: err=%v", lev, err)
		}
	}
}

func TestMergeFutKlines(t *testing.T) {
	d := decimal.NewFromInt
	idx := []klineBar{{Open: 0, O: d(1), H: d(1), L: d(1), C: d(1), V: d(9)}, {Open: 60, O: d(2), H: d(2), L: d(2), C: d(2)}}
	tr := []klineBar{{Open: 60, O: d(5), H: d(5), L: d(5), C: d(5), V: d(3), T: d(15)}, {Open: 120, O: d(6), H: d(6), L: d(6), C: d(6), V: d(1), T: d(6)}}
	out, src := mergeFutKlines(tr, idx, 0)
	if src != "mixed" || len(out) != 3 {
		t.Fatalf("src=%s n=%d", src, len(out))
	}
	if out[0]["source"] != "index" || !out[0]["volume"].(decimal.Decimal).IsZero() {
		t.Fatalf("index bar: %+v", out[0])
	}
	if _, isIdx := out[1]["source"]; isIdx || !out[1]["close"].(decimal.Decimal).Equal(d(5)) {
		t.Fatalf("trade must win bucket: %+v", out[1])
	}
	if out, src := mergeFutKlines(tr, idx, 1); len(out) != 1 || src != "trade" || out[0]["openTime"] != int64(120000) {
		t.Fatalf("limit: %s %+v", src, out)
	}
	if _, src := mergeFutKlines(nil, idx, 0); src != "index" {
		t.Fatal(src)
	}
	if _, src := mergeFutKlines(nil, nil, 0); src != "none" {
		t.Fatal(src)
	}
	if klineSec["1w"] != 604800 {
		t.Fatal("1w interval missing")
	}
}
