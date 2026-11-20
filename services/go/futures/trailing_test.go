package main

import (
	"database/sql"
	"testing"

	"github.com/shopspring/decimal"
)

func trailingOrder(side string, cbRate, cbAmt, activation string) AlgoOrder {
	a := AlgoOrder{Kind: "trailing_stop", Side: side}
	if cbRate != "" {
		a.CallbackRate = decimal.NullDecimal{Decimal: d(cbRate), Valid: true}
	}
	if cbAmt != "" {
		a.CallbackAmount = decimal.NullDecimal{Decimal: d(cbAmt), Valid: true}
	}
	if activation != "" {
		a.Activation = decimal.NullDecimal{Decimal: d(activation), Valid: true}
	}
	return a
}

// sell trailing (close LONG): rate-based activation, peak chases upward, fires
// when the price retraces by cb pct from the peak.
func TestUpdateTrailing_SellRate(t *testing.T) {
	a := trailingOrder("sell", "0.01", "", "100000")
	peak, activated, fire := updateTrailing(a, d("99000"))
	if activated || fire {
		t.Fatalf("below activation should not activate: got activated=%v fire=%v peak=%s", activated, fire, peak)
	}
	peak, activated, fire = updateTrailing(a, d("100000"))
	if !activated || fire {
		t.Fatalf("reached activation should activate: got activated=%v fire=%v peak=%s", activated, fire, peak)
	}
	// After activation: peak tracks upward, retrace 1% = fire
	a.ActivatedAt = sql.NullTime{Valid: true}
	a.Peak = decimal.NullDecimal{Decimal: peak, Valid: true}
	peak, _, fire = updateTrailing(a, d("101000"))
	if fire || !peak.Equal(d("101000")) {
		t.Fatalf("new high should raise peak without firing: peak=%s fire=%v", peak, fire)
	}
	a.Peak = decimal.NullDecimal{Decimal: peak, Valid: true}
	// 1% retrace = 101000 * (1 - 0.01) = 99990
	peak, _, fire = updateTrailing(a, d("99990"))
	if !fire {
		t.Fatalf("1%% retrace should fire: peak=%s", peak)
	}
	a.Peak = decimal.NullDecimal{Decimal: d("101000"), Valid: true}
	peak, _, fire = updateTrailing(a, d("100500"))
	if fire {
		t.Fatalf("small retrace should not fire: peak=%s", peak)
	}
}

// buy trailing (close SHORT): peak chases downward, fires when the price
// rebounds by cb pct from the trough.
func TestUpdateTrailing_BuyRate(t *testing.T) {
	a := trailingOrder("buy", "0.02", "", "")
	// No activation price: immediate activation on first tick (handled by caller)
	a.ActivatedAt = sql.NullTime{Valid: true}
	a.Peak = decimal.NullDecimal{Decimal: d("100000"), Valid: true}
	peak, _, fire := updateTrailing(a, d("99000"))
	if fire || !peak.Equal(d("99000")) {
		t.Fatalf("new low should lower peak without firing: peak=%s fire=%v", peak, fire)
	}
	a.Peak = decimal.NullDecimal{Decimal: peak, Valid: true}
	// 2% rebound from 99000 = 100980
	_, _, fire = updateTrailing(a, d("100980"))
	if !fire {
		t.Fatal("2% rebound should fire")
	}
}

// Absolute callback amount (price delta instead of fraction).
func TestUpdateTrailing_Amount(t *testing.T) {
	a := trailingOrder("sell", "", "500", "")
	a.ActivatedAt = sql.NullTime{Valid: true}
	a.Peak = decimal.NullDecimal{Decimal: d("100000"), Valid: true}
	_, _, fire := updateTrailing(a, d("99501"))
	if fire {
		t.Fatal("499 below peak should not fire")
	}
	_, _, fire = updateTrailing(a, d("99500"))
	if !fire {
		t.Fatal("500 below peak should fire")
	}
}

func TestTrailingReq_RateBounds(t *testing.T) {
	s := &Server{cfg: Config{}, contracts: map[string]Contract{"BTCUSDT": {Symbol: "BTCUSDT", Status: "trading", Tick: d("0.1"), MinQty: d("0.001")}}}
	_, err := s.CreateTrailing(nil, "u", TrailingReq{Symbol: "BTCUSDT", CallbackRate: "0.0001"})
	if err == nil {
		t.Fatal("too-small rate should be rejected")
	}
	_, err = s.CreateTrailing(nil, "u", TrailingReq{Symbol: "BTCUSDT", CallbackRate: "0.5"})
	if err == nil {
		t.Fatal("too-large rate should be rejected")
	}
	_, err = s.CreateTrailing(nil, "u", TrailingReq{Symbol: "BTCUSDT"})
	if err == nil {
		t.Fatal("missing rate/amount should be rejected")
	}
	_, err = s.CreateTrailing(nil, "u", TrailingReq{Symbol: "BTCUSDT", CallbackRate: "0.01", CallbackAmount: "10"})
	if err == nil {
		t.Fatal("both rate and amount should be rejected")
	}
}
