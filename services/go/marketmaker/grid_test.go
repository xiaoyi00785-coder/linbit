package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/shopspring/decimal"
)

func TestGridStepNiceAndStable(t *testing.T) {
	cases := map[string]string{"61700": "2.5", "61000": "2.5", "51000": "2.5", "50000": "2", "75000": "2.5", "101000": "2.5", "102000": "5", "3000": "0.1"}
	for mid, want := range cases {
		if g := gridStep(d(mid), d("30"), 60, d("0.1")); !g.Equal(d(want)) {
			t.Errorf("gridStep(%s)=%s want %s", mid, g, want)
		}
	}
	if g := gridStep(d("100"), d("30"), 60, d("0.1")); !g.Equal(d("0.1")) {
		t.Errorf("tiny raw clamps to tick, got %s", g)
	}
}

func shiftBook(b ExtBook, by decimal.Decimal) ExtBook {
	var o ExtBook
	for _, l := range b.Bids {
		o.Bids = append(o.Bids, BookLevel{l.Price.Add(by), l.Qty})
	}
	for _, l := range b.Asks {
		o.Asks = append(o.Asks, BookLevel{l.Price.Add(by), l.Qty})
	}
	return o
}

func TestDepthGridShapeAndSweep(t *testing.T) {
	p := btcPair(60, 1, "60")
	book := loadOKX(t, "okx_btc_swap_book_a.json", true)
	mid, qs := BuildDepthGrid(p, btcRules, book, Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero)
	var nb, ns int
	bq, sq := decimal.Zero, decimal.Zero
	for _, q := range qs {
		if !q.Qty.Equal(d("1.15")) {
			t.Fatalf("level qty %s", q.Qty)
		}
		if dist := q.Price.Sub(mid).Abs().Div(mid).Mul(bps1); dist.GreaterThan(d("30")) {
			t.Errorf("%s %s outside band (%s bps)", q.Side, q.Price, dist)
		}
		if q.Side == "buy" {
			nb++
			bq = bq.Add(q.Qty)
			if q.Price.GreaterThan(book.Bids[0].Price) {
				t.Errorf("bid %s above venue bid %s", q.Price, book.Bids[0].Price)
			}
		} else {
			ns++
			sq = sq.Add(q.Qty)
			if q.Price.LessThan(book.Asks[0].Price) {
				t.Errorf("ask %s below venue ask %s", q.Price, book.Asks[0].Price)
			}
		}
		if !q.Price.Mod(d("2.5")).IsZero() {
			t.Errorf("price %s not on the 2.5 grid", q.Price)
		}
	}
	if nb != 60 || ns != 60 || bq.LessThan(d("60")) || sq.LessThan(d("60")) {
		t.Fatalf("levels %d/%d qty %s/%s", nb, ns, bq, sq)
	}
	for _, qty := range []string{"20", "50"} {
		for _, side := range []string{"buy", "sell"} {
			s := sweepMarket(qs, side, d(qty), mid)
			if !s.Filled.Equal(d(qty)) || s.SlipBps.GreaterThan(d("20")) {
				t.Errorf("%s %s: filled %s slip %s", side, qty, s.Filled, s.SlipBps)
			}
			t.Logf("grid sweep %s %s: filled %s vwap slip %s bps worst %s bps levels %d", side, qty, s.Filled, s.SlipBps.StringFixed(2), s.WorstBps.StringFixed(2), s.Levels)
		}
	}
}

func TestDepthGridMoveChangesOnlyEdges(t *testing.T) {
	p := btcPair(60, 1, "60")
	inv := Inventory{AllowBuy: true, AllowSell: true}
	book := loadOKX(t, "okx_btc_swap_book_a.json", true)
	_, q1 := BuildDepthGrid(p, btcRules, book, inv, decimal.Zero)
	var rest []restingOrder
	for i, q := range q1 {
		rest = append(rest, restingOrder{ID: string(rune('a' + i%26)), Side: q.Side, Price: q.Price, Remaining: q.Qty})
	}
	// sub-step move: nothing changes
	_, q0 := BuildDepthGrid(p, btcRules, shiftBook(book, d("0.3")), inv, decimal.Zero)
	if c, pl := DiffExact(rest, q0); len(c)+len(pl) > 4 {
		t.Errorf("0.3 move: cancel %d place %d", len(c), len(pl))
	}
	// one full step up: one level in and one out per side
	_, q2 := BuildDepthGrid(p, btcRules, shiftBook(book, d("2.5")), inv, decimal.Zero)
	c, pl := DiffExact(rest, q2)
	if len(c) != 2 || len(pl) != 2 {
		t.Errorf("2.5 move: cancel %d place %d, want 2/2", len(c), len(pl))
	}
	// 10 steps: 10 per side, the other 50 per side untouched
	_, q3 := BuildDepthGrid(p, btcRules, shiftBook(book, d("25")), inv, decimal.Zero)
	c, pl = DiffExact(rest, q3)
	if len(c) != 20 || len(pl) != 20 {
		t.Errorf("25 move: cancel %d place %d, want 20/20", len(c), len(pl))
	}
	// old Diff with requote 2 bps would keep drifting orders; DiffExact never does
	cOld, plOld := Diff(rest, q2, d("2"))
	t.Logf("same 1-step move with old Diff(requote 2bps): cancel %d place %d (grid would drift)", len(cOld), len(plOld))
}

func TestDiffExactPartialFill(t *testing.T) {
	tq := []Quote{{Side: "buy", Price: d("100"), Qty: d("1")}, {Side: "sell", Price: d("101"), Qty: d("1")}}
	rest := []restingOrder{{ID: "1", Side: "buy", Price: d("100"), Remaining: d("0.6")}, {ID: "2", Side: "sell", Price: d("101.0"), Remaining: d("0.4")}}
	c, p := DiffExact(rest, tq)
	if len(c) != 1 || c[0].ID != "2" || len(p) != 1 || p[0].Side != "sell" {
		t.Fatalf("cancel %v place %v", c, p)
	}
}

func TestDepthGridInventoryAndMargin(t *testing.T) {
	p := btcPair(60, 1, "60")
	book := loadOKX(t, "okx_btc_swap_book_a.json", true)
	_, qs := BuildDepthGrid(p, btcRules, book, Inventory{AllowBuy: false, AllowSell: true}, decimal.Zero)
	for _, q := range qs {
		if q.Side == "buy" {
			t.Fatal("bids quoted while AllowBuy=false")
		}
	}
	_, qs = BuildDepthGrid(p, btcRules, book, Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero)
	if _, f := capMarginBudget(qs, 1, d("10000000"), btcRules); !f.Equal(one) {
		t.Errorf("60 BTC x2 at lev 1 should fit 10M margin, factor %s", f)
	}
}

// MM_SIM_DUMP_GRID=file writes the grid book (OKX snapshot a) for the
// futures-engine sweep test (futures/mm_sweep_integration_test.go).
func TestDumpGridQuotes(t *testing.T) {
	out := os.Getenv("MM_SIM_DUMP_GRID")
	if out == "" {
		t.Skip("MM_SIM_DUMP_GRID not set")
	}
	raw := loadOKX(t, "okx_btc_swap_book_a.json", true)
	_, qs := BuildDepthGrid(btcPair(60, 1, "60"), btcRules, raw, Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero)
	mark := raw.Bids[0].Price.Add(raw.Asks[0].Price).Div(two)
	b, _ := json.Marshal(map[string]any{"mark": mark, "quotes": qs})
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
