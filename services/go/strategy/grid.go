package main

// 网格档位和首仓拆分。每格数量向下取整，初始买入报价向上取整到 8 位。

import (
	"errors"
	"math"

	"github.com/shopspring/decimal"
)

// Levels 返回 n+1 个价格，p_0=lower 到 p_n=upper，按价格精度四舍五入。
// 等差是等宽价差，等比是等比例。四舍五入后若不再严格递增则失败。
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

// Plan 是启动价 P 上的初始布局。
type Plan struct {
	Qty        decimal.Decimal // base quantity per grid order
	BuyLevels  []int           // levels below P (limit buys)
	SellLevels []int           // levels above P (limit sells, funded by the initial buy)
	InitQuote  decimal.Decimal // quote budget of the initial market buy
}

func floorStep(v decimal.Decimal, prec int32) decimal.Decimal { return v.RoundFloor(prec) }

// MakePlan 在启动价 P 上拆投资额：P 以下挂限价买，P 以上的卖单靠一笔初始市价买入备货。
//
//	q = investment / (Σ_{p_i<P} p_i + count(p_i>P)·P·(1+fee+buffer))
//
// 每格数量按数量精度向下取整。初始买入的报价预算向上取整到 8 位。
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
	// 最高档不能挂买，最低档不能挂卖。价格等于 P 的那一档空着，当作网格缺口。
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
		// 初始市价买入的报价预算向上取整到 8 位。
		pl.InitQuote = decimal.NewFromInt(int64(len(pl.SellLevels))).Mul(pl.Qty).Mul(p).Mul(mult).RoundUp(8)
	}
	return pl
}

// GridProfitRange 返回扣双边手续费后的单格利润率区间：相邻档价比乘 (1-fee) 两次再减 1。
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
