package main

import (
	"errors"
	"math"

	"github.com/shopspring/decimal"
)

// Levels returns the n+1 grid prices p_0=lower .. p_n=upper, rounded to the
// symbol's price precision. Arithmetic: equal price step; geometric: equal ratio.
func Levels(mode string, lower, upper decimal.Decimal, n int, prec int32) ([]decimal.Decimal, error) {
	if n < 1 || !upper.GreaterThan(lower) || lower.Sign() <= 0 {
		return nil, errors.New("bad range")
	}
	out := make([]decimal.Decimal, n+1)
	nd := decimal.NewFromInt(int64(n))
	lf, _ := lower.Float64()
	ratio, _ := upper.Div(lower).Float64()
	for i := 0; i <= n; i++ {
		var p decimal.Decimal
		switch {
		case i == 0:
			p = lower
		case i == n:
			p = upper
		case mode == "geometric":
			p = decimal.NewFromFloat(lf * math.Pow(ratio, float64(i)/float64(n)))
		default:
			p = lower.Add(upper.Sub(lower).Mul(decimal.NewFromInt(int64(i))).Div(nd))
		}
		out[i] = p.Round(prec)
		if i > 0 && !out[i].GreaterThan(out[i-1]) {
			return nil, errors.New("levels collapse at this price precision")
		}
	}
	return out, nil
}

// Plan is the initial layout of a grid at start price P.
type Plan struct {
	Qty        decimal.Decimal // base quantity per grid order
	BuyLevels  []int           // levels below P (limit buys)
	SellLevels []int           // levels above P (limit sells, funded by the initial buy)
	InitQuote  decimal.Decimal // quote budget of the initial market buy
}

func floorStep(v decimal.Decimal, prec int32) decimal.Decimal { return v.RoundFloor(prec) }

// MakePlan sizes the grid so that the buy orders below P plus the initial
// market buy for the sell side spend the investment:
//
//	q = investment / (Σ_{p_i<P} p_i + count(p_i>P)·P·(1+fee+buffer))
func MakePlan(levels []decimal.Decimal, p, invest, fee, buffer decimal.Decimal, qprec int32) Plan {
	var pl Plan
	sumBelow := decimal.Zero
	for i, l := range levels {
		switch {
		case l.LessThan(p):
			pl.BuyLevels = append(pl.BuyLevels, i)
			sumBelow = sumBelow.Add(l)
		case l.GreaterThan(p):
			pl.SellLevels = append(pl.SellLevels, i)
		}
	}
	// The top level can never hold a buy and the bottom never a sell; a level
	// equal to P is left empty (it is the grid's "gap").
	if n := len(pl.BuyLevels); n > 0 && pl.BuyLevels[n-1] == len(levels)-1 {
		pl.BuyLevels = pl.BuyLevels[:n-1]
	}
	if len(pl.SellLevels) > 0 && pl.SellLevels[0] == 0 {
		pl.SellLevels = pl.SellLevels[1:]
	}
	mult := decimal.NewFromInt(1).Add(fee).Add(buffer)
	den := sumBelow.Add(decimal.NewFromInt(int64(len(pl.SellLevels))).Mul(p).Mul(mult))
	if den.Sign() <= 0 {
		return pl
	}
	pl.Qty = floorStep(invest.Div(den), qprec)
	if len(pl.SellLevels) > 0 {
		pl.InitQuote = decimal.NewFromInt(int64(len(pl.SellLevels))).Mul(pl.Qty).Mul(p).Mul(mult).RoundUp(8)
	}
	return pl
}

// GridProfitRange returns the min and max per-grid profit rate after fees.
func GridProfitRange(levels []decimal.Decimal, fee decimal.Decimal) (lo, hi decimal.Decimal) {
	one := decimal.NewFromInt(1)
	for i := 1; i < len(levels); i++ {
		r := levels[i].Div(levels[i-1]).Mul(one.Sub(fee)).Mul(one.Sub(fee)).Sub(one)
		if i == 1 || r.LessThan(lo) {
			lo = r
		}
		if i == 1 || r.GreaterThan(hi) {
			hi = r
		}
	}
	return lo, hi
}
