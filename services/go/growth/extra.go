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
}

func (s *Server) afterSyncLocked(ctx context.Context) error {
	return errors.Join(s.accruePoints(ctx), s.inviteKYCRewards(ctx))
}

func (s *Server) Housekeeping(ctx context.Context) { s.expireCoupons(ctx) }
