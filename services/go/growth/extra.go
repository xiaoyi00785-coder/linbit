package main

import (
	"context"
	"net/http"
)

// Hooks for the reward programs (points, coupons, gifts, activities).
func (s *Server) extraRoutes(mux *http.ServeMux) {
	s.couponRoutes(mux)
	s.pointsRoutes(mux)
}

func (s *Server) afterSyncLocked(ctx context.Context) error { return s.accruePoints(ctx) }

func (s *Server) Housekeeping(ctx context.Context) { s.expireCoupons(ctx) }
