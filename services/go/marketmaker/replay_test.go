package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// replay_test.go: replays a real 1-second price path (OKX BTC-USDT-SWAP 1s
// candles, MM_REPLAY=file) through the depth-mode quoting + diff + amend
// pairing, with order operations limited to MM_REPLAY_OPS per second (prod:
// ~13 ops/s for BTCUSDT on 2026-11-23). Venue book = saved OKX snapshot shape
// shifted to each second's close. Reports per-side depth within the band and
// order operations, for v1 (copy+thicken, Diff requote 2 bps) vs v2 (grid).

type replayRow struct {
	T     int64
	Close decimal.Decimal
}

func loadReplay(t *testing.T, file string) []replayRow {
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var v [][]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out := make([]replayRow, 0, len(v))
	for _, r := range v {
		out = append(out, replayRow{T: int64(r[0].(float64)), Close: d(r[4].(string))})
	}
	return out
}

type shapeLv struct{ off, qty decimal.Decimal }

func bookShape(b ExtBook) (bids, asks []shapeLv) {
	mid := b.Bids[0].Price.Add(b.Asks[0].Price).Div(two)
	for _, l := range b.Bids {
		bids = append(bids, shapeLv{l.Price.Sub(mid), l.Qty})
	}
	for _, l := range b.Asks {
		asks = append(asks, shapeLv{l.Price.Sub(mid), l.Qty})
	}
	return
}

func shiftedBook(bids, asks []shapeLv, mid, tick decimal.Decimal) ExtBook {
	var b ExtBook
	for _, l := range bids {
		b.Bids = append(b.Bids, BookLevel{floorTo(mid.Add(l.off), tick), l.qty})
	}
	for _, l := range asks {
		b.Asks = append(b.Asks, BookLevel{ceilTo(mid.Add(l.off), tick), l.qty})
	}
	b.Source = "replay"
	return b
}

type replayStats struct {
	name                           string
	secs                           int
	bidDepth, askDepth             []float64
	amends, cancels, places, fills int
	cycles                         int
	maxOpen                        int
	slip20, slip50                 []float64
}

func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(math.Floor(p * float64(len(s)-1)))
	return s[i]
}

func fracGE(xs []float64, v float64) float64 {
	n := 0
	for _, x := range xs {
		if x >= v-1e-9 {
			n++
		}
	}
	return float64(n) / float64(len(xs))
}

func runReplay(t *testing.T, name string, rows []replayRow, p Pair, grid bool, opsPerSec float64, sb, sa []shapeLv) replayStats {
	st := replayStats{name: name}
	inv := Inventory{AllowBuy: true, AllowSell: true}
	px := func(sec int) decimal.Decimal {
		if sec >= len(rows) {
			sec = len(rows) - 1
		}
		return rows[sec].Close
	}
	var resting []restingOrder
	nextID := 0
	opDur := 1 / opsPerSec
	t0 := 0.0
	sampled := 0 // next integer second to sample
	band := p.DepthBandBps.InexactFloat64()
	sample := func(upto float64) {
		for float64(sampled) <= upto && sampled < len(rows) {
			mid := px(sampled)
			bb := mid.Sub(d("0.05"))
			ba := mid.Add(d("0.05"))
			// stale quotes through the venue touch get taken (arb / taker)
			keep := resting[:0]
			for _, o := range resting {
				if (o.Side == "buy" && o.Price.GreaterThanOrEqual(ba)) || (o.Side == "sell" && o.Price.LessThanOrEqual(bb)) {
					st.fills++
					continue
				}
				keep = append(keep, o)
			}
			resting = keep
			var bd, ad float64
			m := mid.InexactFloat64()
			var qs []Quote
			for _, o := range resting {
				pf := o.Price.InexactFloat64()
				dist := math.Abs(pf/m-1) * 1e4
				if dist > band {
					continue
				}
				if o.Side == "buy" && pf < m {
					bd += o.Remaining.InexactFloat64()
				}
				if o.Side == "sell" && pf > m {
					ad += o.Remaining.InexactFloat64()
				}
				qs = append(qs, Quote{Side: o.Side, Price: o.Price, Qty: o.Remaining})
			}
			st.bidDepth = append(st.bidDepth, bd)
			st.askDepth = append(st.askDepth, ad)
			if sampled%10 == 0 {
				s20b := sweepMarket(qs, "buy", d("20"), mid)
				s50b := sweepMarket(qs, "buy", d("50"), mid)
				s20s := sweepMarket(qs, "sell", d("20"), mid)
				s50s := sweepMarket(qs, "sell", d("50"), mid)
				st.slip20 = append(st.slip20, math.Max(s20b.SlipBps.InexactFloat64(), s20s.SlipBps.InexactFloat64()))
				st.slip50 = append(st.slip50, math.Max(s50b.SlipBps.InexactFloat64(), s50s.SlipBps.InexactFloat64()))
			}
			if len(resting) > st.maxOpen {
				st.maxOpen = len(resting)
			}
			sampled++
		}
	}
	end := float64(len(rows) - 1)
	for t0 < end {
		sample(t0)
		st.cycles++
		book := shiftedBook(sb, sa, px(int(t0)), btcRules.Tick)
		var targets []Quote
		var cancel []restingOrder
		var place []Quote
		if grid {
			_, targets = BuildDepthGrid(p, btcRules, book, inv, decimal.Zero)
			targets, _ = capMarginBudget(targets, p.Leverage, d("10000000"), btcRules)
			cancel, place = DiffExact(resting, targets)
		} else {
			_, targets = BuildFromBook(p, btcRules, truncBook(book, depthLimit(p.Levels)), inv, decimal.Zero, one)
			targets, _ = capMarginBudget(targets, p.Leverage, d("10000000"), btcRules)
			cancel, place = Diff(resting, targets, p.RequoteBps)
		}
		sort.SliceStable(place, func(i, j int) bool { return place[i].Level < place[j].Level })
		// amend pairing as applyOps (futures, MM_FUTURES_AMEND)
		type op struct {
			kind string
			o    restingOrder
			q    Quote
		}
		var ops []op
		usedP := make([]bool, len(place))
		var restC []restingOrder
		for _, o := range cancel {
			j := -1
			for k, q := range place {
				if !usedP[k] && q.Side == o.Side {
					j = k
					break
				}
			}
			if j < 0 {
				restC = append(restC, o)
				continue
			}
			usedP[j] = true
			ops = append(ops, op{"amend", o, place[j]})
		}
		for _, o := range restC {
			ops = append(ops, op{kind: "cancel", o: o})
		}
		for k, q := range place {
			if !usedP[k] {
				ops = append(ops, op{kind: "place", q: q})
			}
		}
		tc := t0
		for _, x := range ops {
			tc += opDur
			sample(tc - 1e-9)
			switch x.kind {
			case "amend":
				st.amends++
				for i := range resting {
					if resting[i].ID == x.o.ID {
						resting[i].Price, resting[i].Remaining = x.q.Price, x.q.Qty
					}
				}
			case "cancel":
				st.cancels++
				for i := range resting {
					if resting[i].ID == x.o.ID {
						resting = append(resting[:i], resting[i+1:]...)
						break
					}
				}
			case "place":
				st.places++
				nextID++
				resting = append(resting, restingOrder{ID: fmt.Sprint(nextID), Side: x.q.Side, Price: x.q.Price, Remaining: x.q.Qty})
			}
		}
		// fast refill (MM_REFILL_MIN_MS=300): checked on scheduler ticks (MM_TICK_MS=500)
		next := math.Max(t0+1, tc)
		for tr := math.Max(tc, t0+0.5); tr < t0+1; tr += 0.5 {
			sample(tr)
			if needsRefill(resting, targets) {
				next = tr
				break
			}
		}
		t0 = next
	}
	sample(end)
	// drop the first 15 s (initial build from an empty book)
	if len(st.bidDepth) > 15 {
		st.bidDepth, st.askDepth = st.bidDepth[15:], st.askDepth[15:]
	}
	st.secs = len(st.bidDepth)
	return st
}

func TestReplayDepthModes(t *testing.T) {
	file := os.Getenv("MM_REPLAY")
	if file == "" {
		t.Skip("MM_REPLAY not set")
	}
	rows := loadReplay(t, file)
	if from, to := os.Getenv("MM_REPLAY_FROM"), os.Getenv("MM_REPLAY_TO"); from != "" {
		var f, e int64
		fmt.Sscan(from, &f)
		fmt.Sscan(to, &e)
		var sel []replayRow
		for _, r := range rows {
			if r.T >= f && (e == 0 || r.T < e) {
				sel = append(sel, r)
			}
		}
		rows = sel
	}
	ops := 13.0
	if v := os.Getenv("MM_REPLAY_OPS"); v != "" {
		fmt.Sscan(v, &ops)
	}
	sb, sa := bookShape(truncBook(loadOKX(t, "okx_btc_swap_book_a.json", true), 400))
	v1 := btcPair(200, 1, "60")
	v2 := btcPair(60, 1, "60")
	var rep strings.Builder
	var path float64
	for i := 1; i < len(rows); i++ {
		path += math.Abs(rows[i].Close.Sub(rows[i-1].Close).InexactFloat64())
	}
	fmt.Fprintf(&rep, "\nreplay %d s (%s .. %s), first %s last %s, path length %.1f USDT, ops cap %.0f/s\n", len(rows),
		fmtMs(rows[0].T), fmtMs(rows[len(rows)-1].T), rows[0].Close, rows[len(rows)-1].Close, path, ops)
	fmt.Fprintf(&rep, "%-34s %7s %7s %7s %7s %7s %7s | %7s %7s %7s %6s %7s %6s | %6s %6s\n", "mode", "bidMin", "bidP5", "bidP50", "askMin", "askP5", "askP50",
		"≥60%", "amend/m", "cncl/m", "plc/m", "ops/s", "maxOpn", "s20p95", "s50p95")
	for _, c := range []struct {
		name string
		p    Pair
		grid bool
	}{{"v1 copy+thicken 200lv requote2", v1, false}, {"v2 grid 60lv x 1.15 BTC", v2, true}} {
		s := runReplay(t, c.name, rows, c.p, c.grid, ops, sb, sa)
		min := func(xs []float64) float64 { return pct(xs, 0) }
		both := make([]float64, len(s.bidDepth))
		for i := range both {
			both[i] = math.Min(s.bidDepth[i], s.askDepth[i])
		}
		mins := float64(s.secs) / 60
		fmt.Fprintf(&rep, "%-34s %7.1f %7.1f %7.1f %7.1f %7.1f %7.1f | %6.1f%% %7.1f %7.1f %6.1f %7.2f %6d | %6.2f %6.2f\n", c.name,
			min(s.bidDepth), pct(s.bidDepth, .05), pct(s.bidDepth, .5), min(s.askDepth), pct(s.askDepth, .05), pct(s.askDepth, .5),
			100*fracGE(both, 60), float64(s.amends)/mins, float64(s.cancels)/mins, float64(s.places)/mins,
			float64(s.amends+s.cancels+s.places)/float64(s.secs), s.maxOpen, pct(s.slip20, .95), pct(s.slip50, .95))
		if c.grid {
			if pct(both, 0.01) < 55 {
				t.Errorf("grid: p1 min-side depth %.1f < 55", pct(both, 0.01))
			}
		}
	}
	t.Log(rep.String())
	if out := os.Getenv("MM_REPLAY_OUT"); out != "" {
		f, _ := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.WriteString(rep.String())
		f.Close()
	}
}

func fmtMs(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02dZ", (ms/3600000)%24, (ms/60000)%60, (ms/1000)%60)
}
