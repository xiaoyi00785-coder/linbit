package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var btcRules = Rules{Market: "futures", Symbol: "BTCUSDT", Base: "BTC", Quote: "USDT", Tick: d("0.1"), Step: d("0.0001"), MinQty: d("0.0001"), Trading: true}

func btcPair(levels, lev int, target string) Pair {
	return Pair{Market: "futures", Symbol: "BTCUSDT", Enabled: true, Levels: levels, Leverage: lev, MaxPosition: d("100000000"),
		DepthTargetQty: d(target), DepthBandBps: d("30"), RequoteBps: d("2")}
}

// loadOKX reads a saved OKX books response; ct=true converts contracts to BTC.
func loadOKX(t *testing.T, file string, ct bool) ExtBook {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Data []struct{ Bids, Asks [][]string } `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, err := parseSides(v.Data[0].Bids, v.Data[0].Asks, "okx")
	if err != nil {
		t.Fatal(err)
	}
	if ct {
		b = scaleBook(b, d("0.01"))
	}
	return b
}

type sweep struct {
	Filled, VWAP, SlipBps, WorstBps decimal.Decimal
	Levels                          int
}

// sweepMarket mimics futures match(): best price first, stop at mark±5%.
func sweepMarket(qs []Quote, side string, qty, mark decimal.Decimal) sweep {
	opp := "sell"
	if side == "sell" {
		opp = "buy"
	}
	var book []Quote
	for _, q := range qs {
		if q.Side == opp {
			book = append(book, q)
		}
	}
	sort.SliceStable(book, func(i, j int) bool {
		if opp == "sell" {
			return book[i].Price.LessThan(book[j].Price)
		}
		return book[i].Price.GreaterThan(book[j].Price)
	})
	band := mark.Mul(d("0.05"))
	var s sweep
	rem, cost := qty, decimal.Zero
	for _, q := range book {
		if !rem.IsPositive() || q.Price.Sub(mark).Abs().GreaterThan(band) {
			break
		}
		x := decimal.Min(rem, q.Qty)
		rem, cost, s.Filled = rem.Sub(x), cost.Add(x.Mul(q.Price)), s.Filled.Add(x)
		s.Levels++
		s.WorstBps = q.Price.Sub(mark).Abs().Div(mark).Mul(bps1)
	}
	if s.Filled.IsPositive() {
		s.VWAP = cost.Div(s.Filled)
		s.SlipBps = s.VWAP.Sub(mark).Abs().Div(mark).Mul(bps1)
	}
	return s
}

func extQuotes(b ExtBook) []Quote {
	var out []Quote
	for i, l := range b.Bids {
		out = append(out, Quote{Side: "buy", Price: l.Price, Qty: l.Qty, Level: i})
	}
	for i, l := range b.Asks {
		out = append(out, Quote{Side: "sell", Price: l.Price, Qty: l.Qty, Level: i})
	}
	return out
}

func notional(qs []Quote, side string) decimal.Decimal {
	n := decimal.Zero
	for _, q := range qs {
		if q.Side == side {
			n = n.Add(q.Qty.Mul(q.Price))
		}
	}
	return n
}

func TestOKXSwapContractsConvertedToBase(t *testing.T) {
	var inst int32
	ox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/public/instruments") {
			atomic.AddInt32(&inst, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "0", "data": []map[string]any{{"ctVal": "0.01", "ctValCcy": "BTC"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "0", "data": []map[string]any{{
			"bids": [][]string{{"60000", "500"}}, "asks": [][]string{{"60000.1", "300"}}}}})
	}))
	defer ox.Close()
	f := newBookFeed(Config{BookFeed: "okx", BookScale: d("1"), BookTTL: time.Millisecond, OKXURL: ox.URL})
	f.client = http.DefaultClient
	for i := 0; i < 2; i++ {
		b, err := f.fetchOKX(context.Background(), "BTC-USDT-SWAP", 5)
		if err != nil || !b.Bids[0].Qty.Equal(d("5")) || !b.Asks[0].Qty.Equal(d("3")) {
			t.Fatalf("want 500/300 contracts -> 5/3 BTC, got %+v %v", b, err)
		}
	}
	if inst != 1 {
		t.Fatalf("ctVal should be cached, instruments hits=%d", inst)
	}
	// spot ids are already base units
	b, err := f.fetchOKX(context.Background(), "BTC-USDT", 5)
	if err != nil || !b.Bids[0].Qty.Equal(d("500")) {
		t.Fatalf("spot must not be scaled: %+v %v", b, err)
	}
}

func TestOKXSwapWithoutCtValFails(t *testing.T) {
	ox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/public/instruments") {
			http.Error(w, "down", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "0", "data": []map[string]any{{
			"bids": [][]string{{"60000", "500"}}, "asks": [][]string{{"60000.1", "300"}}}}})
	}))
	defer ox.Close()
	f := newBookFeed(Config{BookFeed: "okx", BookScale: d("1"), OKXURL: ox.URL})
	f.client = http.DefaultClient
	if _, err := f.fetchOKX(context.Background(), "BTC-USDT-SWAP", 5); err == nil {
		t.Fatal("unknown contract value must not copy contracts as BTC")
	}
}

func TestDepthLimits(t *testing.T) {
	for in, want := range map[int]int{100: 100, 150: 200, 200: 200, 300: 400} {
		if g := depthLimit(in); g != want {
			t.Fatalf("depthLimit(%d)=%d want %d", in, g, want)
		}
	}
	for in, want := range map[int]int{100: 100, 200: 500, 400: 500, 2000: 1000} {
		if g := binanceDepthLimit(in); g != want {
			t.Fatalf("binanceDepthLimit(%d)=%d want %d", in, g, want)
		}
	}
}

func TestDepthModeOffKeepsOldCap(t *testing.T) {
	p := btcPair(100, 10, "0")
	if got := quoteNotionalCap(p); !got.Equal(d("3000")) {
		t.Fatalf("depth mode off: cap %s want 3000", got)
	}
	p.DepthTargetQty = d("60")
	if got := quoteNotionalCap(p); !got.Equal(bookNotionalMax()) {
		t.Fatalf("depth mode on: cap %s", got)
	}
}

func TestApplyDepthTargetThickensFarAndCapsSide(t *testing.T) {
	// thin book: 10 asks of 0.5 BTC, 1 bp apart, mid 60000
	mid := d("60000")
	var asks []Quote
	for i := 0; i < 10; i++ {
		asks = append(asks, Quote{Side: "sell", Price: mid.Mul(one.Add(decimal.NewFromInt(int64(i + 1)).Div(bps1))), Qty: d("0.5"), Level: i})
	}
	p := btcPair(100, 10, "60")
	out := applyDepthTarget(asks, "sell", mid, p, btcRules)
	tot := sideQty(out)
	if tot.LessThan(d("59.9")) || tot.GreaterThan(d("90")) {
		t.Fatalf("side total %s, want ~60 (<= 90)", tot)
	}
	if !out[0].Price.Equal(asks[0].Price) || out[0].Qty.GreaterThan(out[len(out)-1].Qty) {
		t.Fatalf("best level must stay at venue price and far levels must be thicker: first %+v last %+v", out[0], out[len(out)-1])
	}
	edge := mid.Mul(d("1.003"))
	for i, q := range out {
		if q.Price.GreaterThan(edge) {
			t.Fatalf("level %d beyond band edge: %s", i, q.Price)
		}
		if i > 0 && !q.Price.GreaterThan(out[i-1].Price) {
			t.Fatalf("asks not ascending at %d", i)
		}
	}
	// thick book: total capped at 1.5x target
	var thick []Quote
	for i := 0; i < 50; i++ {
		thick = append(thick, Quote{Side: "sell", Price: mid.Add(decimal.NewFromInt(int64(i + 1))), Qty: d("5"), Level: i})
	}
	if tot := sideQty(applyDepthTarget(thick, "sell", mid, p, btcRules)); !tot.Equal(d("90")) {
		t.Fatalf("thick side total %s want 90", tot)
	}
}

func TestCapMarginBudget(t *testing.T) {
	qs := []Quote{{Side: "buy", Price: d("60000"), Qty: d("50")}, {Side: "sell", Price: d("60000"), Qty: d("50")}}
	out, f := capMarginBudget(qs, 10, d("300000"), btcRules) // need 600k
	if !f.Equal(d("0.5")) || !out[0].Qty.Equal(d("25")) {
		t.Fatalf("f=%s %+v", f, out)
	}
	if _, f := capMarginBudget(qs, 10, d("1000000"), btcRules); !f.Equal(one) {
		t.Fatalf("within budget must not scale: %s", f)
	}
}

func TestNeedsRefill(t *testing.T) {
	tg := []Quote{
		{Side: "buy", Price: d("59990"), Qty: d("10")}, {Side: "buy", Price: d("59900"), Qty: d("50")},
		{Side: "sell", Price: d("60010"), Qty: d("10")}, {Side: "sell", Price: d("60100"), Qty: d("50")},
	}
	full := []restingOrder{
		{Side: "buy", Price: d("59990"), Remaining: d("10")}, {Side: "buy", Price: d("59900"), Remaining: d("50")},
		{Side: "sell", Price: d("60010"), Remaining: d("10")}, {Side: "sell", Price: d("60100"), Remaining: d("50")},
	}
	if needsRefill(full, tg) {
		t.Fatal("untouched book must not refill")
	}
	if !needsRefill(full[:3], tg) { // 50 BTC of asks taken
		t.Fatal("swept side must refill")
	}
	topTaken := []restingOrder{full[0], full[1], {Side: "sell", Price: d("60100"), Remaining: d("50")}, {Side: "sell", Price: d("60100"), Remaining: d("5")}}
	if !needsRefill(topTaken, tg) {
		t.Fatal("best ask moved 15 bps: must refill")
	}
}

// TestLargeOrderSimulation replays real OKX BTC-USDT-SWAP depth (2026-11-23)
// through the maker and sweeps 20/50 BTC market orders like futures match().
func TestLargeOrderSimulation(t *testing.T) {
	type sc struct {
		name   string
		ct     bool
		p      Pair
		margin string
	}
	cases := []sc{
		{"A 现网: OKX张数当BTC + 3M/(10x*100) 上限", false, btcPair(100, 10, "0"), ""},
		{"B 仅修 ctVal(仍用旧上限)", true, btcPair(100, 10, "0"), ""},
		{"C 深度模式 100档 目标60BTC/30bps", true, btcPair(100, 10, "60"), "1500000"},
		{"D 深度模式 200档 目标60BTC/30bps", true, btcPair(200, 10, "60"), "1500000"},
	}
	files := []string{"okx_btc_swap_book_a.json", "okx_btc_swap_book_c.json"}
	var rep strings.Builder
	fmt.Fprintf(&rep, "\n%-40s %-6s %-5s %8s %8s %8s %6s %10s %10s %8s\n", "场景", "盘口", "方向", "成交BTC", "均价滑点", "最差bps", "吃档数", "卖盘总BTC", "买盘总BTC", "冻结保证金")
	for _, f := range files {
		okx := extQuotes(truncBook(loadOKX(t, f, true), 400))
		raw := loadOKX(t, f, true)
		mark := raw.Bids[0].Price.Add(raw.Asks[0].Price).Div(two)
		for _, qty := range []string{"20", "50"} {
			for _, side := range []string{"buy", "sell"} {
				s := sweepMarket(okx, side, d(qty), mark)
				fmt.Fprintf(&rep, "%-40s %-6s %-5s %8s %8s %8s %6d\n", "OKX 原盘 400档(参照)", f[len(f)-6:len(f)-5], side+qty, s.Filled.StringFixed(2), s.SlipBps.StringFixed(2), s.WorstBps.StringFixed(2), s.Levels)
			}
		}
		for _, c := range cases {
			book := truncBook(loadOKX(t, f, c.ct), depthLimit(c.p.Levels))
			_, qs := BuildFromBook(c.p, btcRules, book, Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero, one)
			if c.margin != "" {
				qs, _ = capMarginBudget(qs, c.p.Leverage, d(c.margin), btcRules)
			}
			asks, bids := decimal.Zero, decimal.Zero
			for _, q := range qs {
				if q.Side == "sell" {
					asks = asks.Add(q.Qty)
				} else {
					bids = bids.Add(q.Qty)
				}
			}
			mg := notional(qs, "buy").Add(notional(qs, "sell")).Div(decimal.NewFromInt(int64(c.p.Leverage)))
			for _, qty := range []string{"20", "50"} {
				for _, side := range []string{"buy", "sell"} {
					s := sweepMarket(qs, side, d(qty), mark)
					fmt.Fprintf(&rep, "%-40s %-6s %-5s %8s %8s %8s %6d %10s %10s %8s\n", c.name, f[len(f)-6:len(f)-5], side+qty, s.Filled.StringFixed(2), s.SlipBps.StringFixed(2), s.WorstBps.StringFixed(2), s.Levels, asks.StringFixed(1), bids.StringFixed(1), mg.StringFixed(0))
					if c.p.DepthTargetQty.IsPositive() {
						if !s.Filled.Equal(d(qty)) {
							t.Errorf("%s %s %s: filled %s", c.name, side, qty, s.Filled)
						}
						if s.SlipBps.GreaterThan(d("20")) {
							t.Errorf("%s %s %s: slippage %s bps > 20", c.name, side, qty, s.SlipBps)
						}
					}
				}
			}
			if c.p.DepthTargetQty.IsPositive() && len(qs) > openOrdersPageMax {
				t.Errorf("%s: %d quotes > open-orders page %d", c.name, len(qs), openOrdersPageMax)
			}
		}
	}
	if out := os.Getenv("MM_SIM_DUMP"); out != "" { // feed for the futures-engine sweep test
		_, qs := BuildFromBook(cases[3].p, btcRules, truncBook(loadOKX(t, files[0], true), depthLimit(200)), Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero, one)
		qs, _ = capMarginBudget(qs, 10, d("1500000"), btcRules)
		raw := loadOKX(t, files[0], true)
		mark := raw.Bids[0].Price.Add(raw.Asks[0].Price).Div(two)
		b, _ := json.Marshal(map[string]any{"mark": mark, "quotes": qs})
		if err := os.WriteFile(out, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// churn: resting = targets from snapshot b, next pass from c (1 s later)
	for _, c := range cases[2:] {
		_, q1 := BuildFromBook(c.p, btcRules, truncBook(loadOKX(t, "okx_btc_swap_book_b.json", true), depthLimit(c.p.Levels)), Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero, one)
		_, q2 := BuildFromBook(c.p, btcRules, truncBook(loadOKX(t, "okx_btc_swap_book_c.json", true), depthLimit(c.p.Levels)), Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero, one)
		var rest []restingOrder
		for i, q := range q1 {
			rest = append(rest, restingOrder{ID: fmt.Sprint(i), Side: q.Side, Price: q.Price, Remaining: q.Qty})
		}
		cn, pl := Diff(rest, q2, c.p.RequoteBps)
		fmt.Fprintf(&rep, "churn %-36s resting %d -> cancel %d / place %d (1s apart, requote 2bps)\n", c.name, len(q1), len(cn), len(pl))
	}
	t.Log(rep.String())
}

func loadBinance(t *testing.T, file string) ExtBook {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Bids, Asks [][]string }
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, err := parseSides(v.Bids, v.Asks, "binance")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestLargeOrderSimulationBinanceProd uses prod's real settings (2026-11-23):
// feed binance first, BTCUSDT futures levels=100, leverage=1, mm balance 63.6M.
func TestLargeOrderSimulationBinanceProd(t *testing.T) {
	book := loadBinance(t, "binance_fapi_btc_book.json")
	mark := book.Bids[0].Price.Add(book.Asks[0].Price).Div(two)
	cases := []struct {
		name   string
		p      Pair
		margin string
	}{
		{"现网 Binance 100档 lev1 cap30k", btcPair(100, 1, "0"), ""},
		{"深度模式 100档 lev1 目标60", btcPair(100, 1, "60"), "10000000"},
		{"深度模式 200档 lev1 目标60", btcPair(200, 1, "60"), "10000000"},
		{"深度模式 200档 lev1 目标60 预算1.5M(默认,不够)", btcPair(200, 1, "60"), "1500000"},
	}
	var rep strings.Builder
	fmt.Fprintf(&rep, "\nBinance FAPI BTCUSDT mid %s\n%-44s %-7s %8s %8s %8s %6s %9s %9s %10s\n", mark.StringFixed(2), "场景", "方向", "成交BTC", "均价bps", "最差bps", "吃档", "卖盘BTC", "买盘BTC", "冻结保证金")
	ext := extQuotes(book)
	for _, qty := range []string{"20", "50"} {
		for _, side := range []string{"buy", "sell"} {
			s := sweepMarket(ext, side, d(qty), mark)
			fmt.Fprintf(&rep, "%-44s %-7s %8s %8s %8s %6d\n", "Binance 原盘 500档(参照)", side+qty, s.Filled.StringFixed(2), s.SlipBps.StringFixed(2), s.WorstBps.StringFixed(2), s.Levels)
		}
	}
	for _, c := range cases {
		_, qs := BuildFromBook(c.p, btcRules, truncBook(book, depthLimit(c.p.Levels)), Inventory{AllowBuy: true, AllowSell: true}, decimal.Zero, one)
		if c.margin != "" {
			qs, _ = capMarginBudget(qs, c.p.Leverage, d(c.margin), btcRules)
		}
		mg := notional(qs, "buy").Add(notional(qs, "sell")).Div(decimal.NewFromInt(int64(c.p.Leverage)))
		var a, b decimal.Decimal
		for _, q := range qs {
			if q.Side == "sell" {
				a = a.Add(q.Qty)
			} else {
				b = b.Add(q.Qty)
			}
		}
		for _, qty := range []string{"20", "50"} {
			for _, side := range []string{"buy", "sell"} {
				s := sweepMarket(qs, side, d(qty), mark)
				fmt.Fprintf(&rep, "%-44s %-7s %8s %8s %8s %6d %9s %9s %10s\n", c.name, side+qty, s.Filled.StringFixed(2), s.SlipBps.StringFixed(2), s.WorstBps.StringFixed(2), s.Levels, a.StringFixed(1), b.StringFixed(1), mg.StringFixed(0))
				if c.margin == "10000000" && (!s.Filled.Equal(d(qty)) || s.SlipBps.GreaterThan(d("20"))) {
					t.Errorf("%s %s %s: filled %s slip %s", c.name, side, qty, s.Filled, s.SlipBps)
				}
			}
		}
	}
	t.Log(rep.String())
}
