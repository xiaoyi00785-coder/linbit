package main

// limits.go：提现前的 KYC 门和滚动 24h 额度。额度按 USDT 等值，先于任何冻结。

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// usdtPrice 取该币的 USDT 价：先钉死价（稳定币），再 market-data 实时价。ok=false 表示没有可用价。
func (s *Server) usdtPrice(ctx context.Context, asset string) (decimal.Decimal, bool) {
	// USDT 自己就是计价单位，没有 USDTUSDT 指数。不在这里钉 1，每笔 USDT 提现都会失败关闭转人工。
	if strings.EqualFold(asset, "USDT") {
		return decimal.NewFromInt(1), true
	}
	if p, ok := s.cfg.KYC.price(asset); ok {
		return p, true
	}
	return s.market.LastPrice(ctx, asset)
}

// checkDailyLimit 在冻结之前执行：
//   - 等级 < MinLevel（默认 1）              → 403 KYC_REQUIRED
//   - 计入额度的币没有 USDT 价               → 400 PRICE_UNAVAILABLE（失败关闭）
//   - 近 24h 已用 + 本笔 > 该等级上限         → 400 WITHDRAW_LIMIT_EXCEEDED
//
// 没配档位时（例如手写 Config 的单测）额度部分不生效。
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
