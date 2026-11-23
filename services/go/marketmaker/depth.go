package main

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// depth.go: depth mode for large futures market orders.
//
// The old per-level cap 3,000,000/(leverage*levels) was sized for the early
// risk tiers (5x -> 5M). With 100 levels at 10x it left ~3,000 USDT per level,
// ~300k USDT (~5 BTC) per side, so a 20–50 BTC market order ran out of book and
// the remainder was cancelled as no_liquidity. In depth mode the venue depth is
// copied as is (per level <= quoteNotionalMax), the side total is held between
// DepthTargetQty and 1.5x of it, and the shortfall within DepthBandBps is added
// to the farther levels (weight grows with distance from mid), so the first
// levels still mirror OKX and the large order pays for walking out.

const (
	depthExtraLevels  = 20  // synthetic levels appended between the copied book and the band edge
	depthMaxSideMult  = 1.5 // side total cap = target * this (bounds margin)
	depthDefaultBand  = 30  // bps
	maxLevelsPerSide  = 200 // futures open-orders listing returns at most 500 rows
	openOrdersPageMax = 500
)

func depthMode(p Pair) bool {
	return strings.EqualFold(p.Market, "futures") && p.DepthTargetQty.IsPositive()
}

func sideQty(qs []Quote) decimal.Decimal {
	t := decimal.Zero
	for _, q := range qs {
		t = t.Add(q.Qty)
	}
	return t
}

// applyDepthTarget reshapes one side (best first) to the depth target.
func applyDepthTarget(qs []Quote, side string, mid decimal.Decimal, p Pair, r Rules) []Quote {
	if len(qs) == 0 || !mid.IsPositive() {
		return qs
	}
	target := p.DepthTargetQty
	band := p.DepthBandBps
	if !band.IsPositive() {
		band = decimal.NewFromInt(depthDefaultBand)
	}
	tick := r.Tick
	var edge decimal.Decimal
	if side == "buy" {
		edge = ceilTo(mid.Mul(one.Sub(band.Div(bps1))), tick)
	} else {
		edge = floorTo(mid.Mul(one.Add(band.Div(bps1))), tick)
	}
	inBand := func(px decimal.Decimal) bool {
		if side == "buy" {
			return px.GreaterThanOrEqual(edge)
		}
		return px.LessThanOrEqual(edge)
	}
	dist := func(px decimal.Decimal) decimal.Decimal { return px.Sub(mid).Abs().Div(mid).Mul(bps1) }

	// 1. cap the side total at target*1.5 (keep the inner levels).
	maxSide := target.Mul(decimal.NewFromFloat(depthMaxSideMult))
	out := make([]Quote, 0, len(qs)+depthExtraLevels)
	cum := decimal.Zero
	for _, q := range qs {
		if cum.GreaterThanOrEqual(maxSide) {
			break
		}
		if cum.Add(q.Qty).GreaterThan(maxSide) {
			q.Qty = floorTo(maxSide.Sub(cum), r.Step)
			if !okQty(q.Qty, q.Price, r) {
				break
			}
		}
		cum = cum.Add(q.Qty)
		out = append(out, q)
	}

	// 2. shortfall within the band.
	have := decimal.Zero
	lastIn := -1
	for i, q := range out {
		if inBand(q.Price) {
			have = have.Add(q.Qty)
			lastIn = i
		}
	}
	short := target.Sub(have)
	if !short.IsPositive() {
		return out
	}
	// synthetic levels from the last in-band price out to the edge
	from := mid
	if lastIn >= 0 {
		from = out[lastIn].Price
	}
	lvl := 0
	if len(out) > 0 {
		lvl = out[len(out)-1].Level + 1
	}
	gap := edge.Sub(from)
	var extra []Quote
	seen := map[string]bool{}
	for _, q := range out {
		seen[q.Price.String()] = true
	}
	for k := 1; k <= depthExtraLevels; k++ {
		px := from.Add(gap.Mul(decimal.NewFromInt(int64(k))).Div(decimal.NewFromInt(depthExtraLevels)))
		if side == "buy" {
			px = floorTo(px, tick)
		} else {
			px = ceilTo(px, tick)
		}
		if !px.IsPositive() || !inBand(px) || seen[px.String()] {
			continue
		}
		seen[px.String()] = true
		extra = append(extra, Quote{Side: side, Price: px, Level: lvl})
		lvl++
	}
	// in-band candidates: copied in-band levels + synthetic ones; weight 1+distBps
	type cand struct {
		idx   int // index in out, or -1-k for extra[k]
		w     decimal.Decimal
		price decimal.Decimal
	}
	var cs []cand
	wsum := decimal.Zero
	for i := 0; i <= lastIn; i++ {
		if inBand(out[i].Price) {
			w := one.Add(dist(out[i].Price))
			cs = append(cs, cand{i, w, out[i].Price})
			wsum = wsum.Add(w)
		}
	}
	for k := range extra {
		w := one.Add(dist(extra[k].Price))
		cs = append(cs, cand{-1 - k, w, extra[k].Price})
		wsum = wsum.Add(w)
	}
	if wsum.IsZero() {
		return out
	}
	levelCap := bookNotionalMax()
	for _, c := range cs {
		add := floorTo(short.Mul(c.w).Div(wsum), r.Step)
		var q *Quote
		if c.idx >= 0 {
			q = &out[c.idx]
		} else {
			q = &extra[-1-c.idx]
		}
		nq := q.Qty.Add(add)
		if capQ := floorTo(levelCap.Div(c.price), r.Step); nq.GreaterThan(capQ) {
			nq = capQ
		}
		q.Qty = nq
	}
	for _, q := range extra {
		if okQty(q.Qty, q.Price, r) {
			out = append(out, q)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if side == "buy" {
			return out[i].Price.GreaterThan(out[j].Price)
		}
		return out[i].Price.LessThan(out[j].Price)
	})
	return out
}

func okQty(q, px decimal.Decimal, r Rules) bool {
	if !q.IsPositive() || (r.MinQty.IsPositive() && q.LessThan(r.MinQty)) {
		return false
	}
	return !r.MinNotional.IsPositive() || !q.Mul(px).LessThan(r.MinNotional)
}

// capMarginBudget scales every quote down when resting both sides would freeze
// more than budget USDT of initial margin (notional/leverage). Returns the
// factor used (1 = unchanged).
func capMarginBudget(qs []Quote, leverage int, budget decimal.Decimal, r Rules) ([]Quote, decimal.Decimal) {
	if !budget.IsPositive() || leverage <= 0 {
		return qs, one
	}
	need := decimal.Zero
	for _, q := range qs {
		need = need.Add(q.Price.Mul(q.Qty))
	}
	need = need.Div(decimal.NewFromInt(int64(leverage)))
	if need.LessThanOrEqual(budget) {
		return qs, one
	}
	f := budget.Div(need)
	out := make([]Quote, 0, len(qs))
	for _, q := range qs {
		q.Qty = floorTo(q.Qty.Mul(f), r.Step)
		if okQty(q.Qty, q.Price, r) {
			out = append(out, q)
		}
	}
	return out, f
}

// needsRefill: in depth mode a fill should not wait for refresh_ms. True when
// either side rests < 85% of its target qty, or its best level moved > 2 bps
// away from the target best (top levels were taken).
func needsRefill(resting []restingOrder, targets []Quote) bool {
	for _, side := range []string{"buy", "sell"} {
		var tq, rq decimal.Decimal
		var tBest, rBest decimal.Decimal
		for _, t := range targets {
			if t.Side != side {
				continue
			}
			tq = tq.Add(t.Qty)
			if tBest.IsZero() || (side == "buy" && t.Price.GreaterThan(tBest)) || (side == "sell" && t.Price.LessThan(tBest)) {
				tBest = t.Price
			}
		}
		if tq.IsZero() {
			continue
		}
		for _, o := range resting {
			if o.Side != side {
				continue
			}
			rq = rq.Add(o.Remaining)
			if rBest.IsZero() || (side == "buy" && o.Price.GreaterThan(rBest)) || (side == "sell" && o.Price.LessThan(rBest)) {
				rBest = o.Price
			}
		}
		if rq.LessThan(tq.Mul(decimal.RequireFromString("0.85"))) || rBest.IsZero() {
			return true
		}
		if rBest.Sub(tBest).Abs().Div(tBest).Mul(bps1).GreaterThan(decimal.NewFromInt(2)) {
			return true
		}
	}
	return false
}
