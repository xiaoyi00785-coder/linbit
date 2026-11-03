package main

import (
	"github.com/shopspring/decimal"
)

var (
	bps1 = decimal.NewFromInt(10000)
	one  = decimal.NewFromInt(1)
	two  = decimal.NewFromInt(2)
)

// Pair is one market-making configuration row.
type Pair struct {
	Market          string          `json:"market"`
	Symbol          string          `json:"symbol"`
	Enabled         bool            `json:"enabled"`
	SpreadBps       decimal.Decimal `json:"spreadBps"`
	Levels          int             `json:"levels"`
	LevelStepBps    decimal.Decimal `json:"levelStepBps"`
	LevelNotional   decimal.Decimal `json:"levelNotional"`
	SizeGrowth      decimal.Decimal `json:"sizeGrowth"`
	RefreshMs       int             `json:"refreshMs"`
	RequoteBps      decimal.Decimal `json:"requoteBps"`
	TargetBaseRatio decimal.Decimal `json:"targetBaseRatio"`
	MaxPosition     decimal.Decimal `json:"maxPosition"`
	MaxSkewBps      decimal.Decimal `json:"maxSkewBps"`
	Leverage        int             `json:"leverage"`
	UpdatedBy       string          `json:"updatedBy"`
	UpdatedAt       string          `json:"updatedAt"`
}

// Quote is one target order.
type Quote struct {
	Side  string          `json:"side"`
	Price decimal.Decimal `json:"price"`
	Qty   decimal.Decimal `json:"qty"`
	Level int             `json:"level"`
}

func clamp1(x decimal.Decimal) decimal.Decimal {
	if x.GreaterThan(one) {
		return one
	}
	if x.LessThan(one.Neg()) {
		return one.Neg()
	}
	return x
}

func floorTo(x, step decimal.Decimal) decimal.Decimal {
	if !step.IsPositive() {
		return x
	}
	return x.Div(step).Floor().Mul(step)
}

func ceilTo(x, step decimal.Decimal) decimal.Decimal {
	if !step.IsPositive() {
		return x
	}
	return x.Div(step).Ceil().Mul(step)
}

// Inventory describes where the MM stands on a pair.
//   - imbalance in [-1,1]: >0 means too long (too much base / long position);
//     the mid is shifted down by imbalance*maxSkew so asks fill sooner.
//   - allowBuy/allowSell: hard caps (max position) disable one side.
type Inventory struct {
	Imbalance decimal.Decimal `json:"imbalance"`
	AllowBuy  bool            `json:"allowBuy"`
	AllowSell bool            `json:"allowSell"`
	BaseValue decimal.Decimal `json:"baseValue"` // spot: base value in quote; futures: signed position notional
	// spot only: max_position applies to the NET position (base - baseline),
	// so seeded / treasury inventory does not block bids.
	BaseQty        decimal.Decimal `json:"baseQty"`
	Baseline       decimal.Decimal `json:"baseline"`
	BaselineSource string          `json:"baselineSource,omitempty"`
	NetQty         decimal.Decimal `json:"netQty"`
	NetValue       decimal.Decimal `json:"netValue"`
}

// spotInventory: base value vs. quote held, against the target base ratio.
// The max_position cap applies to the net position base - baseline (the part
// built by MM trading): net long >= cap stops bids, net short <= -cap stops asks.
func spotInventory(p Pair, index, base, quote decimal.Decimal, bl Baseline) Inventory {
	bv := base.Mul(index)
	net := base.Sub(bl.Qty)
	nv := net.Mul(index)
	inv := Inventory{AllowBuy: true, AllowSell: base.IsPositive(), BaseValue: bv,
		BaseQty: base, Baseline: bl.Qty, BaselineSource: bl.Source, NetQty: net, NetValue: nv}
	total := bv.Add(quote)
	if total.IsPositive() {
		ratio := bv.Div(total)
		t := p.TargetBaseRatio
		den := decimal.Max(t, one.Sub(t))
		if den.IsPositive() {
			inv.Imbalance = clamp1(ratio.Sub(t).Div(den))
		}
	}
	if p.MaxPosition.IsPositive() && nv.GreaterThanOrEqual(p.MaxPosition) {
		inv.AllowBuy = false
	}
	if p.MaxPosition.IsPositive() && nv.LessThanOrEqual(p.MaxPosition.Neg()) {
		inv.AllowSell = false
	}
	if !quote.IsPositive() {
		inv.AllowBuy = false
	}
	return inv
}

// futuresInventory: signed position notional vs. max position.
func futuresInventory(p Pair, index, pos decimal.Decimal) Inventory {
	n := pos.Mul(index)
	inv := Inventory{AllowBuy: true, AllowSell: true, BaseValue: n}
	if p.MaxPosition.IsPositive() {
		inv.Imbalance = clamp1(n.Div(p.MaxPosition))
		if n.GreaterThanOrEqual(p.MaxPosition) {
			inv.AllowBuy = false
		}
		if n.LessThanOrEqual(p.MaxPosition.Neg()) {
			inv.AllowSell = false
		}
	}
	return inv
}

// BuildLadder returns the target quotes for one pair. Bids are rounded down and
// asks up to the tick so the spread never gets tighter than configured.
func BuildLadder(p Pair, r Rules, index decimal.Decimal, inv Inventory) (decimal.Decimal, []Quote) {
	mid := index.Mul(one.Sub(inv.Imbalance.Mul(p.MaxSkewBps).Div(bps1)))
	var out []Quote
	size := p.LevelNotional
	for i := 0; i < p.Levels; i++ {
		off := p.SpreadBps.Div(two).Add(p.LevelStepBps.Mul(decimal.NewFromInt(int64(i)))).Div(bps1)
		bid := floorTo(mid.Mul(one.Sub(off)), r.Tick)
		ask := ceilTo(mid.Mul(one.Add(off)), r.Tick)
		// never cross the index on the wrong side even under heavy skew
		if bid.GreaterThanOrEqual(index) {
			bid = floorTo(index.Mul(one.Sub(off)), r.Tick)
		}
		if ask.LessThanOrEqual(index) {
			ask = ceilTo(index.Mul(one.Add(off)), r.Tick)
		}
		for _, q := range []Quote{{Side: "buy", Price: bid, Level: i}, {Side: "sell", Price: ask, Level: i}} {
			if (q.Side == "buy" && !inv.AllowBuy) || (q.Side == "sell" && !inv.AllowSell) || !q.Price.IsPositive() {
				continue
			}
			q.Qty = floorTo(size.Div(q.Price), r.Step)
			if !q.Qty.IsPositive() || (r.MinQty.IsPositive() && q.Qty.LessThan(r.MinQty)) {
				continue
			}
			if r.MinNotional.IsPositive() && q.Qty.Mul(q.Price).LessThan(r.MinNotional) {
				continue
			}
			out = append(out, q)
		}
		size = size.Mul(p.SizeGrowth)
	}
	return mid, out
}

// Diff matches resting orders to targets. A resting order is kept when a target
// of the same side is within requoteBps of its price (and not already taken);
// everything else is cancelled and the unmatched targets are placed.
func Diff(resting []restingOrder, targets []Quote, requoteBps decimal.Decimal) (cancel []restingOrder, place []Quote) {
	used := make([]bool, len(targets))
	for _, o := range resting {
		match := -1
		for i, t := range targets {
			if used[i] || t.Side != o.Side || !t.Price.IsPositive() {
				continue
			}
			dev := o.Price.Sub(t.Price).Abs().Div(t.Price).Mul(bps1)
			if dev.LessThanOrEqual(requoteBps) && o.Remaining.GreaterThanOrEqual(t.Qty.Div(two)) {
				match = i
				break
			}
		}
		if match >= 0 {
			used[match] = true
		} else {
			cancel = append(cancel, o)
		}
	}
	for i, t := range targets {
		if !used[i] {
			place = append(place, t)
		}
	}
	return
}
