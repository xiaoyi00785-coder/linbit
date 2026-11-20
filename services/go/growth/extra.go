package main

import (
	"context"
	"errors"
	"net/http"
)

// Hooks for the reward programs (points, coupons, gifts, activities).
func (s *Server) extraRoutes(mux *http.ServeMux) {
	s.couponRoutes(mux)
	s.pointsRoutes(mux)
	s.newcomerRoutes(mux)
	s.taskRoutes(mux)
	s.shopRoutes(mux)
	s.rebateRoutes(mux)
	s.extendReferralRoutes(mux)
}

func (s *Server) afterSyncLocked(ctx context.Context) error {
	return errors.Join(
		s.accruePoints(ctx),
		s.inviteKYCRewards(ctx),
		s.rebateAfterSync(ctx),
	)
}

func (s *Server) Housekeeping(ctx context.Context) {
	s.expireCoupons(ctx)
	_, _ = s.payPendingReferralWithdrawals(ctx)
}
