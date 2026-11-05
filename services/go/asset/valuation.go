package main

import (
	"context"
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// valuation.go: GET /internal/valuation?userId= — the user's total assets in
// USDT across funding + spot + futures, for callers that must decide on the
// number (e.g. account's VIP eligibility check).
//
// Unlike GET /assets/overview (which shows what it can for display), this is
// strict about completeness: when any held asset has no usable index price, or
// the futures wallet could not be read, `complete` is false and the total is
// only a lower bound. A caller may still accept a lower bound that already
// clears its threshold, but must fail closed otherwise.

// GET /internal/valuation?userId=
func (s *Server) handleInternalValuation(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 userId")
		return
	}
	ctx := r.Context()
	if err := s.store.EnsureAccount(ctx, userID); err != nil {
		log.Printf("valuation: ensure: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	spot, err := s.store.WalletBalances(ctx, AcctSpot, userID)
	if err != nil {
		log.Printf("valuation: spot: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	funding, err := s.store.WalletBalances(ctx, AcctFunding, userID)
	if err != nil {
		log.Printf("valuation: funding: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	cache := map[string]*decimal.Decimal{}
	fut, futOK := s.futuresWallet(ctx, userID)
	total, complete := s.strictTotal(ctx, cache, funding, spot, []Balance{fut})
	if !futOK {
		complete = false
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"userId": userID, "totalValueUsdt": total.StringFixed(2), "complete": complete,
	})
}

// strictTotal sums every non-zero balance at its USDT index price. complete is
// false when any non-zero balance could not be priced (it is then skipped).
func (s *Server) strictTotal(ctx context.Context, cache map[string]*decimal.Decimal, groups ...[]Balance) (decimal.Decimal, bool) {
	total, complete := decimal.Zero, true
	for _, bs := range groups {
		for _, b := range bs {
			amt := b.Total()
			if amt.IsZero() {
				continue
			}
			p := s.indexPrice(ctx, b.Asset, cache)
			if p == nil {
				complete = false
				continue
			}
			total = total.Add(amt.Mul(*p))
		}
	}
	return total, complete
}
