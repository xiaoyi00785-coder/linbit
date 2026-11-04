package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// usdtPrice resolves an asset's USDT price: pinned prices (stablecoins) first,
// then market-data's real-time last price. ok=false means no price is available.
func (s *Server) usdtPrice(ctx context.Context, asset string) (decimal.Decimal, bool) {
	// USDT is the quote unit itself: there is no USDTUSDT index, so without this
	// every USDT withdrawal would fail closed to manual review.
	if strings.EqualFold(asset, "USDT") {
		return decimal.NewFromInt(1), true
	}
	if p, ok := s.cfg.KYC.price(asset); ok {
		return p, true
	}
	return s.market.LastPrice(ctx, asset)
}

// checkDailyLimit enforces the KYC gate and the KYC-tiered rolling-24h
// withdrawal limit (USDT-equivalent), before any funds are frozen:
//   - level < MinLevel (default 1)          → 403 KYC_REQUIRED
//   - no USDT price for a counted asset      → 400 PRICE_UNAVAILABLE (fail closed)
//   - 24h usage + this amount > level's cap  → 400 WITHDRAW_LIMIT_EXCEEDED
//
// The limit part is a no-op when no tiers are configured (e.g. unit tests).
func (s *Server) checkDailyLimit(ctx context.Context, userID, asset string, amount decimal.Decimal) *apiError {
	_, tiersOn := s.cfg.KYC.limit(0)
	if s.cfg.KYC.MinLevel <= 0 && !tiersOn {
		return nil
	}
	level, err := s.account.KYCLevel(ctx, userID)
	if err != nil {
		return errf(http.StatusInternalServerError, "INTERNAL", "无法获取认证等级")
	}
	if level < s.cfg.KYC.MinLevel {
		return errf(http.StatusForbidden, "KYC_REQUIRED", "请先完成身份认证（KYC）后再提现")
	}
	if !tiersOn {
		return nil
	}
	limit, on := s.cfg.KYC.limit(level)
	if !on || limit.Sign() <= 0 {
		return nil // unlimited for this level
	}
	price, ok := s.usdtPrice(ctx, asset)
	if !ok {
		return errf(http.StatusBadRequest, "PRICE_UNAVAILABLE", "暂无 "+asset+" 的实时价格，无法计算提现额度，请稍后再试")
	}

	since := time.Now().Add(-24 * time.Hour)
	prior, err := s.store.WithdrawnSince(ctx, userID, since)
	if err != nil {
		return errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	used := decimal.Zero
	cache := map[string]decimal.Decimal{asset: price}
	for _, w := range prior {
		p, seen := cache[w.Asset]
		if !seen {
			var ok bool
			if p, ok = s.usdtPrice(ctx, w.Asset); !ok {
				return errf(http.StatusBadRequest, "PRICE_UNAVAILABLE", "暂无 "+w.Asset+" 的实时价格，无法计算提现额度，请稍后再试")
			}
			cache[w.Asset] = p
		}
		used = used.Add(w.Amount.Mul(p))
	}
	projected := used.Add(amount.Mul(price))
	if projected.GreaterThan(limit) {
		return errf(http.StatusBadRequest, "WITHDRAW_LIMIT_EXCEEDED",
			"超过当前认证等级的 24 小时提现额度（上限 "+limit.String()+" USDT 等值，已用 "+used.StringFixed(2)+"）")
	}
	return nil
}
