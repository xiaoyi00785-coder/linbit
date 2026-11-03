package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// GET /growth/config — public, read-only program numbers so marketing copy
// (invite rebate %, newcomer gift total, invite KYC reward) is rendered from
// the same config the settlement uses instead of hardcoded strings.
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
