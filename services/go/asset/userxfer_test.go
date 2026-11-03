package main

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestUserTransferIdempotentAndBalanced(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	a, b := testUser(), testUser()+"b"
	if err := s.Credit(ctx, a, "USDT", decimal.NewFromInt(100), "seed-"+a); err != nil {
		t.Fatal(err)
	}
	ref := "subxfer:" + a
	if err := s.UserTransfer(ctx, a, b, "USDT", AcctSpot, decimal.NewFromInt(40), ref); err != nil {
		t.Fatal(err)
	}
	if err := s.UserTransfer(ctx, a, b, "USDT", AcctSpot, decimal.NewFromInt(40), ref); !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("replay: %v", err)
	}
	if err := s.UserTransfer(ctx, a, b, "USDT", AcctSpot, decimal.NewFromInt(1000), ref+"x"); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("overdraw: %v", err)
	}
	la, _ := s.LedgerBalance(ctx, a, "USDT")
	lb, _ := s.LedgerBalance(ctx, b, "USDT")
	if !la.Equal(decimal.NewFromInt(60)) || !lb.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("ledger a=%s b=%s", la, lb)
	}
}
