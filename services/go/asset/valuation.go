package main

// valuation.go：GET /internal/valuation?userId=。资金账户 + 现货 + 合约的 USDT 总资产，
// 给必须拿这个数做决定的调用方（例如账户服务的 VIP 资格）。
// 和 /assets/overview 不同，这里要求完整：任一持仓没有可用指数价，或合约钱包读不到，
// complete 为 false，总额只是下界。下界已经超过阈值可以接受，否则必须失败关闭。

import (
	"context"
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

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
