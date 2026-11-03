package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestValidateBalanced(t *testing.T) {
	ok := []ledgerLine{
		{Account: "user:1:free", Asset: "USDT", Direction: "debit", Amount: d("10")},
		{Account: "system:external", Asset: "USDT", Direction: "credit", Amount: d("10")},
	}
	if err := validateBalanced(ok); err != nil {
		t.Fatalf("balanced transaction rejected: %v", err)
	}

	unbalanced := []ledgerLine{
		{Account: "user:1:free", Asset: "USDT", Direction: "debit", Amount: d("10")},
		{Account: "system:external", Asset: "USDT", Direction: "credit", Amount: d("9")},
	}
	if err := validateBalanced(unbalanced); err == nil {
		t.Fatal("unbalanced transaction accepted")
	}

	badAmount := []ledgerLine{
		{Account: "user:1:free", Asset: "USDT", Direction: "debit", Amount: d("0")},
		{Account: "system:external", Asset: "USDT", Direction: "credit", Amount: d("0")},
	}
	if err := validateBalanced(badAmount); err == nil {
		t.Fatal("non-positive amount accepted")
	}
}
