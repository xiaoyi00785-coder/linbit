package main

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

// TestSettleIdempotent verifies that settling the same tradeId twice applies
// balances exactly once (idempotency via the settlements unique constraint) and
// that a single settlement conserves value across buyer, seller and the fee
// account, with ledger reconciliation.
func TestSettleIdempotent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	buyer := testUser()
	seller := testUser()
	platform := testUser() // stand-in fee account for test isolation

	// Fund: buyer has 1000 USDT frozen for the order; seller has 0.01 BTC frozen.
	mustCredit(t, store, buyer, "USDT", "1000")
	mustFreeze(t, store, buyer, "USDT", "700") // limit buy 0.01 @ 70000
	mustCredit(t, store, seller, "BTC", "0.01")
	mustFreeze(t, store, seller, "BTC", "0.01")

	req := SettleRequest{
		TradeID:        "trade-1",
		Base:           "BTC",
		Quote:          "USDT",
		BuyerUserID:    buyer,
		SellerUserID:   seller,
		FeeAccount:     platform,
		QuoteAmount:    decimal.RequireFromString("700"),
		BaseQty:        decimal.RequireFromString("0.01"),
		BuyerFeeBase:   decimal.RequireFromString("0.00001"), // 0.1% taker on base
		SellerFeeQuote: decimal.RequireFromString("0.7"),     // 0.1% maker on quote
	}

	// First settle applies.
	if err := store.Settle(ctx, req); err != nil {
		t.Fatalf("first settle: %v", err)
	}
	// Re-delivering the same tradeId must be a no-op.
	if err := store.Settle(ctx, req); err != ErrAlreadySettled {
		t.Fatalf("second settle: got %v, want ErrAlreadySettled", err)
	}
	// A third time too.
	if err := store.Settle(ctx, req); err != ErrAlreadySettled {
		t.Fatalf("third settle: got %v, want ErrAlreadySettled", err)
	}

	// Buyer: USDT free 300 (1000-700), locked 0; BTC free 0.00999.
	assertBalance(t, store, buyer, "USDT", "300", "0")
	assertBalance(t, store, buyer, "BTC", "0.00999", "0")
	// Seller: USDT free 699.3 (700-0.7); BTC locked 0.
	assertBalance(t, store, seller, "USDT", "699.3", "0")
	assertBalance(t, store, seller, "BTC", "0", "0")
	// Platform fee account: 0.7 USDT + 0.00001 BTC.
	assertBalance(t, store, platform, "USDT", "0.7", "0")
	assertBalance(t, store, platform, "BTC", "0.00001", "0")

	// Conservation: total BTC across the three == 0.01; total USDT == 1000.
	btc := bal(t, store, buyer, "BTC").Add(bal(t, store, seller, "BTC")).Add(bal(t, store, platform, "BTC"))
	if !btc.Equal(decimal.RequireFromString("0.01")) {
		t.Fatalf("BTC not conserved: %s", btc)
	}
	usdt := bal(t, store, buyer, "USDT").Add(bal(t, store, seller, "USDT")).Add(bal(t, store, platform, "USDT"))
	if !usdt.Equal(decimal.RequireFromString("1000")) {
		t.Fatalf("USDT not conserved: %s", usdt)
	}
}

// --- helpers ---

func mustCredit(t *testing.T, s *Store, uid, asset, amt string) {
	t.Helper()
	if err := s.EnsureAccount(context.Background(), uid); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := s.Credit(context.Background(), uid, asset, decimal.RequireFromString(amt), "seed"); err != nil {
		t.Fatalf("credit %s %s: %v", uid, asset, err)
	}
}

func mustFreeze(t *testing.T, s *Store, uid, asset, amt string) {
	t.Helper()
	if err := s.Freeze(context.Background(), uid, asset, decimal.RequireFromString(amt), "order"); err != nil {
		t.Fatalf("freeze %s %s: %v", uid, asset, err)
	}
}

func bal(t *testing.T, s *Store, uid, asset string) decimal.Decimal {
	t.Helper()
	bals, _, err := s.Balances(context.Background(), uid, 1, 100)
	if err != nil {
		t.Fatalf("balances: %v", err)
	}
	b := findAsset(bals, asset)
	if b == nil {
		return decimal.Zero
	}
	return b.Total()
}

func assertBalance(t *testing.T, s *Store, uid, asset, wantFree, wantLocked string) {
	t.Helper()
	bals, _, err := s.Balances(context.Background(), uid, 1, 100)
	if err != nil {
		t.Fatalf("balances: %v", err)
	}
	b := findAsset(bals, asset)
	if b == nil {
		if wantFree == "0" && wantLocked == "0" {
			return
		}
		t.Fatalf("%s %s: no balance, want free=%s locked=%s", uid, asset, wantFree, wantLocked)
	}
	if !b.Free.Equal(decimal.RequireFromString(wantFree)) || !b.Locked.Equal(decimal.RequireFromString(wantLocked)) {
		t.Fatalf("%s %s: got free=%s locked=%s, want free=%s locked=%s",
			uid, asset, b.Free, b.Locked, wantFree, wantLocked)
	}
}
