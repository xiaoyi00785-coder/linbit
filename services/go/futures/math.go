package main

import "github.com/shopspring/decimal"

// math.go: pure position / margin / funding arithmetic (unit tested).

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

// applyTrade applies a signed fill d at price px to a one-way position (q, e).
// It returns the new size/entry, the realized PnL and the closed quantity
// (absolute). Opening adds at a size-weighted average entry; reducing realizes
// PnL against the entry; crossing zero closes fully and opens the rest at px.
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

// upnl is the unrealized PnL of position (q, e) at mark m.
func upnl(q, e, m decimal.Decimal) decimal.Decimal { return q.Mul(m.Sub(e)) }

// liqPrice solves  M + q(P - e) = mmr*|q|*P  for P, where M is the margin backing
// the position (isolated margin, or cross equity excluding this position net of
// the other positions' maintenance margin). Returns 0 when there is no solution
// (e.g. fully collateralized long).
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

// fundingRate: rate = P + clamp(I - P, -c, c), then capped to ±cap, where P is
// the average premium index over the interval and I the interest rate.
func fundingRate(avgPremium, interest, clampV, capV decimal.Decimal) decimal.Decimal {
	r := avgPremium.Add(clamp(interest.Sub(avgPremium), clampV.Neg(), clampV))
	return clamp(r, capV.Neg(), capV).Round(8)
}

// markPrice = median(index*(1+lastFunding*timeLeft/interval), index, last).
// Without a last trade the mark is the funding-basis price.
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

// isStep reports whether v is a positive multiple of step.
func isStep(v, step decimal.Decimal) bool {
	if v.Sign() <= 0 || step.Sign() <= 0 {
		return false
	}
	return v.Mod(step).IsZero()
}
