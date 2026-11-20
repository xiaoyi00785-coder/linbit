package main

import (
	"math"
	"testing"
)

// Reference values were cross-checked with py_vollib
// (black_scholes_merton with q=0) and QuantLib for s=100, k=100, t=1, r=0.05,
// v=0.2 and a few bracketing cases. See docs/api/options.md for the script.

type bsCase struct {
	s, k, t, r, v         float64
	isCall                bool
	price                 float64
	delta, gamma, vega    float64
	theta                 float64 // per day
	tol                   float64
}

var bsCases = []bsCase{
	// ATM 1y 20% vol call: canonical Hull textbook result.
	{s: 100, k: 100, t: 1, r: 0.05, v: 0.20, isCall: true,
		price: 10.450584, delta: 0.636831, gamma: 0.018762, vega: 0.375240, theta: -0.017573, tol: 1e-4},
	// ATM 1y 20% vol put (same inputs → put-call parity price 5.5735).
	{s: 100, k: 100, t: 1, r: 0.05, v: 0.20, isCall: false,
		price: 5.573526, delta: -0.363169, gamma: 0.018762, vega: 0.375240, theta: -0.004542, tol: 1e-4},
	// 30d ATM BTC-style: s=60000, k=60000, t≈0.0822, r=0.05, v=0.6
	{s: 60000, k: 60000, t: 30.0 / 365, r: 0.05, v: 0.60, isCall: true,
		price: 4228.134664, delta: 0.543755, gamma: 3.842e-5, vega: 68.210760, theta: -72.100782, tol: 1e-2},
	// Deep ITM call → delta ~1, price ~ S - K e^-rT.
	{s: 150, k: 100, t: 0.5, r: 0.05, v: 0.20, isCall: true,
		price: 52.474591, delta: 0.999079, gamma: 0.000147, vega: 0.003312, theta: -0.013522, tol: 1e-4},
}

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %.6f want %.6f (tol %.4f)", name, got, want, tol)
	}
}

func TestBSPrice(t *testing.T) {
	for _, c := range bsCases {
		p := BSPrice(c.s, c.k, c.t, c.r, c.v, c.isCall)
		approx(t, "price", p, c.price, c.tol)
	}
}

func TestBSGreeks(t *testing.T) {
	for _, c := range bsCases {
		g := BSGreeks(c.s, c.k, c.t, c.r, c.v, c.isCall)
		approx(t, "delta", g.Delta, c.delta, c.tol)
		approx(t, "gamma", g.Gamma, c.gamma, c.tol)
		// vega: cases above list per-1% move; BSGreeks already returns that.
		approx(t, "vega", g.Vega, c.vega, c.tol)
		approx(t, "theta", g.Theta, c.theta, c.tol)
	}
}

// Put-call parity: C - P = S - K e^(-rT), model-free.
func TestPutCallParity(t *testing.T) {
	for _, tc := range []struct{ s, k, tt, r, v float64 }{
		{100, 100, 1, 0.05, 0.2},
		{60000, 70000, 0.3, 0.05, 0.6},
		{5, 10, 2, 0.03, 0.9},
	} {
		c := BSPrice(tc.s, tc.k, tc.tt, tc.r, tc.v, true)
		p := BSPrice(tc.s, tc.k, tc.tt, tc.r, tc.v, false)
		lhs := c - p
		rhs := tc.s - tc.k*math.Exp(-tc.r*tc.tt)
		if math.Abs(lhs-rhs) > 1e-6 {
			t.Errorf("put-call parity broken: %v %v (lhs=%.6f rhs=%.6f)", tc, tc, lhs, rhs)
		}
	}
}

// Degenerate inputs: expiry, zero vol → discounted intrinsic.
func TestBSPriceDegenerate(t *testing.T) {
	if p := BSPrice(100, 90, 0, 0.05, 0.2, true); p != 10 {
		t.Errorf("expiry call: got %v want 10", p)
	}
	if p := BSPrice(100, 110, 0, 0.05, 0.2, false); p != 10 {
		t.Errorf("expiry put: got %v want 10", p)
	}
	if p := BSPrice(100, 100, 1, 0.05, 0, true); math.Abs(p-(100-100*math.Exp(-0.05))) > 1e-9 {
		t.Errorf("zero-vol call: got %v", p)
	}
}

// Round-trip: price → implied vol → price. Must recover the input vol.
func TestImpliedVolRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		s, k, tt, r, v float64
		isCall         bool
	}{
		{100, 100, 1, 0.05, 0.20, true},
		{100, 90, 0.5, 0.05, 0.35, true},
		{100, 110, 0.5, 0.05, 0.35, false},
		{60000, 65000, 30.0 / 365, 0.05, 0.80, true},
	} {
		price := BSPrice(tc.s, tc.k, tc.tt, tc.r, tc.v, tc.isCall)
		iv, ok := BSImpliedVol(price, tc.s, tc.k, tc.tt, tc.r, tc.isCall)
		if !ok {
			t.Errorf("no IV for %+v", tc)
			continue
		}
		if math.Abs(iv-tc.v) > 1e-3 {
			t.Errorf("IV round-trip %+v: got %.4f want %.4f", tc, iv, tc.v)
		}
	}
}

func TestInitialMarginShort(t *testing.T) {
	// ATM short call: ~ 15% of underlying + premium.
	m := InitialMarginShort(60000, 60000, 2000, true)
	if m < 9000 || m > 12000 {
		t.Errorf("ATM short call margin out of expected band: %.2f", m)
	}
	// Deep-OTM short put: margin floors at 10% of strike + tiny premium.
	m = InitialMarginShort(60000, 40000, 10, false)
	floor := 0.10 * 40000
	if m < floor || m > floor+100 {
		t.Errorf("deep-OTM short put margin %.2f not at 10%% floor %.2f", m, floor)
	}
}
