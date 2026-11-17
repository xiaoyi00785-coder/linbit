package main

import (
	"strings"

	"github.com/shopspring/decimal"
)

// BookLevel is one price/qty from an external order book (best first).
type BookLevel struct {
	Price decimal.Decimal
	Qty   decimal.Decimal
}

// ExtBook is a snapshot of an external venue's book.
type ExtBook struct {
	Bids   []BookLevel
	Asks   []BookLevel
	Source string
}

func (b ExtBook) Valid() bool {
	return len(b.Bids) > 0 && len(b.Asks) > 0 &&
		b.Bids[0].Price.IsPositive() && b.Asks[0].Price.IsPositive() &&
		b.Bids[0].Price.LessThan(b.Asks[0].Price)
}

func bookNotionalMin() decimal.Decimal { return decimal.NewFromFloat(quoteNotionalMin) }
func bookNotionalMax() decimal.Decimal { return decimal.NewFromFloat(quoteNotionalMax) }

// BuildFromBook copies the external depth onto our tick/step, scaled so each
// level's notional stays in [100, 1_000_000] USDT. Inventory caps drop a side;
// imbalance shifts every price (same sign as the synthetic ladder). extraBps
// pushes bids down and asks up (TradFi reference deviation).
func BuildFromBook(p Pair, r Rules, book ExtBook, inv Inventory, extraBps, scale decimal.Decimal) (decimal.Decimal, []Quote) {
	if !book.Valid() {
		return decimal.Zero, nil
	}
	if !scale.IsPositive() {
		scale = one
	}
	n := p.Levels
	if n < 1 {
		n = 1
	}
	maxN := quoteNotionalCap(p)
	shift := inv.Imbalance.Mul(p.MaxSkewBps).Div(bps1)
	extra := extraBps.Div(bps1)
	if extra.IsNegative() {
		extra = decimal.Zero
	}

	adj := func(side string, px decimal.Decimal) decimal.Decimal {
		if side == "buy" {
			return floorTo(px.Mul(one.Sub(shift).Sub(extra)), r.Tick)
		}
		return ceilTo(px.Mul(one.Sub(shift).Add(extra)), r.Tick)
	}

	bestBid := adj("buy", book.Bids[0].Price)
	bestAsk := adj("sell", book.Asks[0].Price)
	if !bestBid.IsPositive() || !bestAsk.IsPositive() || !bestBid.LessThan(bestAsk) {
		return decimal.Zero, nil
	}
	mid := bestBid.Add(bestAsk).Div(two)

	emit := func(side string, levels []BookLevel) []Quote {
		if (side == "buy" && !inv.AllowBuy) || (side == "sell" && !inv.AllowSell) {
			return nil
		}
		var out []Quote
		for i, lv := range levels {
			if i >= n || !lv.Price.IsPositive() || !lv.Qty.IsPositive() {
				continue
			}
			px := adj(side, lv.Price)
			if !px.IsPositive() {
				continue
			}
			if side == "buy" && px.GreaterThanOrEqual(bestAsk) {
				continue
			}
			if side == "sell" && px.LessThanOrEqual(bestBid) {
				continue
			}
			q := scaledQty(px, lv.Qty, r, scale, maxN)
			if !q.IsPositive() {
				continue
			}
			out = append(out, Quote{Side: side, Price: px, Qty: q, Level: i})
		}
		return out
	}

	out := emit("buy", book.Bids)
	out = append(out, emit("sell", book.Asks)...)
	return mid, out
}

func quoteNotionalCap(p Pair) decimal.Decimal {
	cap := bookNotionalMax()
	if strings.EqualFold(p.Market, "futures") && p.Leverage > 0 && p.Levels > 0 {
		// Stay under typical USDT-M notional bands at this leverage (e.g. 5x → 5M).
		lim := decimal.NewFromInt(3_000_000).Div(decimal.NewFromInt(int64(p.Leverage * p.Levels)))
		if lim.LessThan(cap) {
			cap = lim
		}
	}
	if cap.LessThan(bookNotionalMin()) {
		return bookNotionalMin()
	}
	return cap
}

func scaledQty(price, extQty decimal.Decimal, r Rules, scale, maxNotional decimal.Decimal) decimal.Decimal {
	if !maxNotional.IsPositive() {
		maxNotional = bookNotionalMax()
	}
	notional := price.Mul(extQty).Mul(scale)
	if notional.LessThan(bookNotionalMin()) {
		notional = bookNotionalMin()
	}
	if notional.GreaterThan(maxNotional) {
		notional = maxNotional
	}
	q := floorTo(notional.Div(price), r.Step)
	if !q.IsPositive() || (r.MinQty.IsPositive() && q.LessThan(r.MinQty)) {
		return decimal.Zero
	}
	if r.MinNotional.IsPositive() && q.Mul(price).LessThan(r.MinNotional) {
		return decimal.Zero
	}
	return q
}
