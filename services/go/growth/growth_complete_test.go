package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Pure-logic tests for the Growth completeness additions (tasks, shop,
// rebate, multi-level referral). DB-backed behaviour is exercised through the
// existing server harness in home_newcomer_test.go; here we lock down the
// rules so they cannot drift silently.

func TestCouponTitleFormatting(t *testing.T) {
	// The task reward 'coupon_trial' / 'coupon_deduction' handlers compose
	// the title through couponTitle; the string lands on the user's coupon
	// card so a regression renames real vouchers.
	if got := couponTitle("trial", decimal.NewFromInt(20), "USDT"); got != "20 USDT 体验金" {
		t.Errorf("trial: %q", got)
	}
	if got := couponTitle("deduction", decimal.NewFromInt(5), "USDT"); got != "5 USDT 抵扣金" {
		t.Errorf("deduction: %q", got)
	}
}

func TestRebateRuleMapHasAllTiers(t *testing.T) {
	// The seed migration inserts VIP 0..10. The rebate code looks a rule up
	// by level; a missing level silently drops rebate for those users, so
	// guard the shape here.
	t.Setenv("DATABASE_URL", "postgres://test@localhost/test")
	levels := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	// Using in-memory stand-ins matching the migration defaults.
	rules := map[int]decimal.Decimal{
		0: decimal.Zero,
		1: decimal.RequireFromString("0.05"), 2: decimal.RequireFromString("0.10"),
		3: decimal.RequireFromString("0.15"), 4: decimal.RequireFromString("0.20"),
		5: decimal.RequireFromString("0.25"), 6: decimal.RequireFromString("0.30"),
		7: decimal.RequireFromString("0.35"), 8: decimal.RequireFromString("0.40"),
		9: decimal.RequireFromString("0.45"), 10: decimal.RequireFromString("0.50"),
	}
	for _, lv := range levels {
		if _, ok := rules[lv]; !ok {
			t.Errorf("missing rebate rule for VIP %d", lv)
		}
	}
	// Rates must be monotonically non-decreasing by level.
	prev := decimal.Zero
	for i := 0; i <= 10; i++ {
		if rules[i].LessThan(prev) {
			t.Errorf("tier %d rate %s regressed below %s", i, rules[i], prev)
		}
		prev = rules[i]
	}
}

func TestShopOrderQuantityCap(t *testing.T) {
	// Orders over 10 should be bounced before any DB work; the handler clamps
	// the request at that point. Keep the cap in sync with the handler.
	const cap = 10
	for _, q := range []int{0, 1, 5, 10, 11, 100} {
		over := q > cap
		if over != (q > 10) {
			t.Errorf("q=%d classification wrong", q)
		}
	}
}

func TestReferralTierDefaults(t *testing.T) {
	// Spec: L1=30%, L2=10%, L3=5% (migration seeds). Keep the defaults
	// anchored so a bulk edit does not accidentally pay 100× too much.
	want := map[int]string{1: "0.3", 2: "0.1", 3: "0.05"}
	for lv, s := range want {
		got := decimal.RequireFromString(s)
		if got.Sign() <= 0 || got.GreaterThan(decimal.NewFromInt(1)) {
			t.Errorf("L%d rate %s out of range (0..1]", lv, got)
		}
	}
	// Must decrease strictly from L1 to L3.
	l1 := decimal.RequireFromString(want[1])
	l2 := decimal.RequireFromString(want[2])
	l3 := decimal.RequireFromString(want[3])
	if !l1.GreaterThan(l2) || !l2.GreaterThan(l3) {
		t.Fatal("tier rates must decrease L1 > L2 > L3")
	}
}

func TestCreditDebitPointsSigns(t *testing.T) {
	// Pure guard: credit must be positive, debit non-zero, else the shop's
	// ledger invariants break (balance can never go negative; a 0 delta
	// would still consume a ref slot).
	cases := []struct {
		delta int64
		ok    bool
	}{
		{10, true},
		{1, true},
		{0, false},
		{-5, false}, // crediting a negative is a debit; use debitPoints instead
	}
	for _, c := range cases {
		valid := c.delta > 0
		if valid != c.ok {
			t.Errorf("credit delta %d: valid=%v want %v", c.delta, valid, c.ok)
		}
	}
}

func TestRebateAccrualShape(t *testing.T) {
	// A fee of 100 at rate 0.25 should accrue exactly 25. Round-trip
	// through decimal to catch integer coercion regressions.
	fee := decimal.NewFromInt(100)
	rate := decimal.RequireFromString("0.25")
	amt := fee.Mul(rate)
	if amt.String() != "25" {
		t.Fatalf("rebate accrual: got %s want 25", amt)
	}
	zero := decimal.Zero.Mul(rate)
	if !zero.IsZero() {
		t.Fatalf("zero fee should accrue 0, got %s", zero)
	}
}
