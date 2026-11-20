package main

import "math"

// blackscholes.go: European option pricing and Greeks.
//
// All math runs in float64 for performance; API boundaries convert to/from
// shopspring/decimal. Conventions:
//
//   S  spot (underlying)                    > 0
//   K  strike                               > 0
//   T  time to expiry in years              > 0
//   r  risk-free rate (decimal, e.g. 0.05)  any sign
//   v  volatility, annualised               >= 0
//
// Theta is returned per calendar day, vega per 1% volatility move, matching
// the way Deribit (and the UI) display them.

const sqrt2 = 1.4142135623730951

// normPDF is the standard normal probability density φ(x).
func normPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}

// normCDF is the standard normal cumulative distribution N(x).
func normCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/sqrt2))
}

// bsD1 returns the Black-Scholes d1 term.
func bsD1(s, k, t, r, v float64) float64 {
	return (math.Log(s/k) + (r+0.5*v*v)*t) / (v * math.Sqrt(t))
}

// BSPrice returns the Black-Scholes European option premium. isCall=false → put.
// Degenerate inputs collapse to intrinsic value so the pricer is total.
func BSPrice(s, k, t, r, v float64, isCall bool) float64 {
	if s <= 0 || k <= 0 {
		return 0
	}
	if t <= 0 || v <= 0 {
		// At or past expiry (or zero vol): fall back to discounted intrinsic.
		fwd := s - k*math.Exp(-r*t)
		if isCall {
			return math.Max(0, fwd)
		}
		return math.Max(0, -fwd)
	}
	d1 := bsD1(s, k, t, r, v)
	d2 := d1 - v*math.Sqrt(t)
	if isCall {
		return s*normCDF(d1) - k*math.Exp(-r*t)*normCDF(d2)
	}
	return k*math.Exp(-r*t)*normCDF(-d2) - s*normCDF(-d1)
}

// Greeks bundles delta, gamma, theta and vega for one option, in the units
// the UI shows: theta per day, vega per 1% vol.
type Greeks struct {
	Delta, Gamma, Theta, Vega float64
}

// BSGreeks returns the Greeks for one European option.
func BSGreeks(s, k, t, r, v float64, isCall bool) Greeks {
	if s <= 0 || k <= 0 || t <= 0 || v <= 0 {
		return Greeks{}
	}
	d1 := bsD1(s, k, t, r, v)
	d2 := d1 - v*math.Sqrt(t)
	pdf := normPDF(d1)
	disc := math.Exp(-r * t)
	gamma := pdf / (s * v * math.Sqrt(t))
	vega := s * pdf * math.Sqrt(t) // per 1.00 vol move
	// Theta per year (continuous). Convert to per calendar day below.
	var delta, thetaYr float64
	if isCall {
		delta = normCDF(d1)
		thetaYr = -(s*pdf*v)/(2*math.Sqrt(t)) - r*k*disc*normCDF(d2)
	} else {
		delta = normCDF(d1) - 1
		thetaYr = -(s*pdf*v)/(2*math.Sqrt(t)) + r*k*disc*normCDF(-d2)
	}
	return Greeks{
		Delta: delta,
		Gamma: gamma,
		Theta: thetaYr / 365,
		Vega:  vega / 100, // per 1% vol
	}
}

// BSImpliedVol back-solves for volatility given a market premium. Returns
// (iv, ok). Uses bisection (robust: no derivative blowups near deep ITM/OTM)
// with a tight tolerance; returns ok=false on out-of-arbitrage inputs.
func BSImpliedVol(price, s, k, t, r float64, isCall bool) (float64, bool) {
	if price <= 0 || s <= 0 || k <= 0 || t <= 0 {
		return 0, false
	}
	// Enforce no-arbitrage bounds; a mark outside them has no real IV.
	disc := math.Exp(-r * t)
	intrinsic := 0.0
	upper := 0.0
	if isCall {
		intrinsic = math.Max(0, s-k*disc)
		upper = s
	} else {
		intrinsic = math.Max(0, k*disc-s)
		upper = k * disc
	}
	if price < intrinsic-1e-9 || price > upper+1e-9 {
		return 0, false
	}
	lo, hi := 1e-4, 5.0 // 0.01% ~ 500% annualised
	for i := 0; i < 100; i++ {
		mid := 0.5 * (lo + hi)
		p := BSPrice(s, k, t, r, mid, isCall)
		if math.Abs(p-price) < 1e-6 {
			return mid, true
		}
		if p < price {
			lo = mid
		} else {
			hi = mid
		}
	}
	return 0.5 * (lo + hi), true
}

// InitialMarginShort returns the USDT held for a short option position
// (one contract). A simplified SPAN-style cover: for calls it's a share of
// the underlying, for puts a share of the strike, plus the current premium.
// Matches the magnitudes Deribit publishes ("0.15 of underlying + premium").
func InitialMarginShort(underlying, strike, premium float64, isCall bool) float64 {
	base := strike
	if isCall {
		base = underlying
	}
	// How far out of the money is the option for its writer's exposure. For a
	// short call, danger is underlying > strike (K - S > 0 means safely OTM).
	// For a short put, danger is underlying < strike (S - K > 0 is OTM).
	var otmAmt float64
	if isCall {
		otmAmt = math.Max(0, strike-underlying)
	} else {
		otmAmt = math.Max(0, underlying-strike)
	}
	reduce := math.Min(otmAmt, base) * 0.5
	floor := 0.10 * base
	span := math.Max(0.15*base-reduce, floor)
	return span + math.Max(premium, 0)
}
