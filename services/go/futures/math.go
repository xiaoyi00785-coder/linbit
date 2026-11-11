package main

// 仓位、保证金、资金费的纯计算。USDT 金额用 money() 四舍五入到 8 位，不截断。
import "github.com/shopspring/decimal"

var (
	dZero = decimal.Zero
	dOne  = decimal.NewFromInt(1)
)

const moneyDP = 8 // USDT amounts are rounded to 8 dp

func money(d decimal.Decimal) decimal.Decimal { return d.Round(moneyDP) }

func dmin(a, b decimal.Decimal) decimal.Decimal {
	if a.LessThan(b) {
		return a
	}
	return b
}

func dmax(a, b decimal.Decimal) decimal.Decimal {
	if a.GreaterThan(b) {
		return a
	}
	return b
}

func clamp(v, lo, hi decimal.Decimal) decimal.Decimal { return dmax(lo, dmin(hi, v)) }

// applyTrade 把带符号的成交量 d 打到仓位 (q, e)。加仓按数量加权更新开仓价；
// 减仓对开仓价实现盈亏；穿过零则先全平，剩余部分以 px 新开。返回新仓、新开仓价、已实现盈亏、平仓量（绝对值）。
func applyTrade(q, e, d, px decimal.Decimal) (nq, ne, pnl, closed decimal.Decimal) {
	if d.IsZero() {
		return q, e, dZero, dZero
	}
	if q.IsZero() || q.Sign() == d.Sign() {
		nq = q.Add(d)
		ne = q.Abs().Mul(e).Add(d.Abs().Mul(px)).Div(nq.Abs())
		return nq, ne, dZero, dZero
	}
	closed = dmin(q.Abs(), d.Abs())
	pnl = closed.Mul(px.Sub(e))
	if q.Sign() < 0 {
		pnl = pnl.Neg()
	}
	nq = q.Add(d)
	switch {
	case nq.IsZero():
		ne = dZero
	case nq.Sign() == q.Sign():
		ne = e
	default:
		ne = px
	}
	return nq, ne, pnl, closed
}

// upnl 用标记价 m，不用指数价：q * (m - e)。空头 q 为负。
func upnl(q, e, m decimal.Decimal) decimal.Decimal { return q.Mul(m.Sub(e)) }

// liqPrice 解 M + q(P-e) = mmr*|q|*P。M 是这笔仓位的保证金：逐仓传 Iso，
// 全仓传钱包余额加上其他全仓未实现盈亏、再减去其他全仓维持保证金。无正数解（例如多头已被全额抵押）返回 0。
func liqPrice(q, e, M, mmr decimal.Decimal) decimal.Decimal {
	if q.IsZero() {
		return dZero
	}
	den := q.Sub(mmr.Mul(q.Abs()))
	if den.IsZero() {
		return dZero
	}
	p := q.Mul(e).Sub(M).Div(den)
	if p.Sign() <= 0 {
		return dZero
	}
	return p
}

// fundingRate：rate = P + clamp(I-P, -c, c)，再夹到 ±cap。P 是区间内溢价指数均值，I 是利率。结果四舍五入到 8 位。
func fundingRate(avgPremium, interest, clampV, capV decimal.Decimal) decimal.Decimal {
	r := avgPremium.Add(clamp(interest.Sub(avgPremium), clampV.Neg(), clampV))
	return clamp(r, capV.Neg(), capV).Round(8)
}

// markPrice 取三者中位数：index*(1+上一期资金费率*剩余时间比例)、index、最新成交价。
// 没有最新成交时标记价就是资金费基差价，不退回指数价本身。
func markPrice(index, last, lastFunding decimal.Decimal, frac decimal.Decimal) decimal.Decimal {
	p1 := index.Mul(dOne.Add(lastFunding.Mul(frac)))
	if last.Sign() <= 0 {
		return p1
	}
	a, b, c := p1, index, last
	if a.GreaterThan(b) {
		a, b = b, a
	}
	if b.GreaterThan(c) {
		b = c
		if a.GreaterThan(b) {
			b = a
		}
	}
	return b
}

// isStep：v 必须是正的 step 整数倍。合约价格/数量校验用它拒绝多余小数，不截断。
func isStep(v, step decimal.Decimal) bool {
	if v.Sign() <= 0 || step.Sign() <= 0 {
		return false
	}
	return v.Mod(step).IsZero()
}
