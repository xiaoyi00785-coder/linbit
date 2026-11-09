package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// GET /growth/config。公开只读的活动数字，邀请返佣比例、新人礼包总额、邀请 KYC 奖励
// 都从结算用的同一份配置渲染，不写死在文案里。
func (s *Server) handlePublicConfig(w http.ResponseWriter, r *http.Request) {
	maxFut, maxSpot := decimal.Zero, decimal.Zero
	for _, t := range s.cfg.Tiers {
		if t.FuturesRate.GreaterThan(maxFut) {
			maxFut = t.FuturesRate
		}
		if t.SpotRate.GreaterThan(maxSpot) {
			maxSpot = t.SpotRate
		}
	}
	nc := s.cfg.Newcomer
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"payoutAsset": s.cfg.PayoutAsset,
		"referral": map[string]any{
			"rate":      s.cfg.ReferralRate.String(), // fraction, 0.10 = 10%
			"validDays": int(s.cfg.ReferralPeriod.Hours() / 24),
		},
		"partner": map[string]any{
			"maxFuturesRate": maxFut.String(),
			"maxSpotRate":    maxSpot.String(),
			"overrideRate":   s.cfg.OverrideRate.String(),
		},
		"inviteKyc": map[string]any{
			"reward":     s.cfg.InviteKYCReward.String(),
			"kycLevel":   s.cfg.InviteKYCLevel,
			"withinDays": int(s.cfg.InviteKYCWithin.Hours() / 24),
		},
		"newcomer": map[string]any{
			"available": len(nc.Tasks) > 0,
			"total":     nc.total().String(),
			"days":      nc.Days,
		},
		"inviteBaseUrl": s.cfg.InviteBaseURL,
	})
}
