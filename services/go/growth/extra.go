package main

import (
	"context"
	"errors"
	"net/http"
)

// 积分、卡券、礼包和活动的挂载点。
func (s *Server) extraRoutes(mux *http.ServeMux) {
	s.couponRoutes(mux)
	s.pointsRoutes(mux)
	s.newcomerRoutes(mux)
}

func (s *Server) afterSyncLocked(ctx context.Context) error {
	return errors.Join(s.accruePoints(ctx), s.inviteKYCRewards(ctx))
}

// Housekeeping 把已过激活截止、仍为 issued 的卡券标成 expired。
func (s *Server) Housekeeping(ctx context.Context) { s.expireCoupons(ctx) }
