package main

import (
	"context"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// checkDailyLimit enforces the KYC-tiered rolling-24h withdrawal limit. It is a
// no-op when the feature is disabled (no limits configured, e.g. unit tests) or
// when the asset has no configured USDT price. The new withdrawal plus the user's
// counted 24h usage must not exceed the limit for their KYC level.
func (s *Server) checkDailyLimit(ctx context.Context, userID, asset string, amount decimal.Decimal) *apiError {
	price, priced := s.cfg.KYC.price(asset)
	if !priced {
		return nil // asset not priced: not subject to the USDT-equivalent limit
	}
	// The feature is off entirely when no tiers are configured.
	if _, on := s.cfg.KYC.limit(0); !on {
		return nil
	}

	level, err := s.account.KYCLevel(ctx, userID)
	if err != nil {
		return errf(http.StatusInternalServerError, "INTERNAL", "无法获取认证等级")
	}
	limit, on := s.cfg.KYC.limit(level)
	if !on || limit.Sign() <= 0 {
		return nil // unlimited for this level
	}

	since := time.Now().Add(-24 * time.Hour)
	prior, err := s.store.WithdrawnSince(ctx, userID, since)
	if err != nil {
		return errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	used := decimal.Zero
	for _, w := range prior {
		if p, ok := s.cfg.KYC.price(w.Asset); ok {
			used = used.Add(w.Amount.Mul(p))
		}
	}
	projected := used.Add(amount.Mul(price))
	if projected.GreaterThan(limit) {
		return errf(http.StatusBadRequest, "WITHDRAW_LIMIT_EXCEEDED",
			"超过当前认证等级的 24 小时提现额度（上限 "+limit.String()+" USDT 等值，已用 "+used.String()+"）")
	}
	return nil
}
