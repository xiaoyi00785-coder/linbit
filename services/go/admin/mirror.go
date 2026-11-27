package main

import (
	"net/http"
)

// Admin proxies for futures position mirror (user view + subsidies).

func (s *Server) mirrorRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	futures := func() string { return s.cfg.FuturesURL }
	a("GET /admin/v1/futures/mirrors", permLedgerView, s.gapRead(futures, "/internal/admin/mirrors"))
	a("GET /admin/v1/futures/mirror-preview", permLedgerView, s.gapRead(futures, "/internal/admin/mirror/account-preview"))
	a("POST /admin/v1/futures/mirrors/enable", permFundAdjust, s.gapWrite(futures, http.MethodPost, "/internal/admin/mirrors/enable", "futures", "position_mirror_enable"))
	a("POST /admin/v1/futures/mirrors/{id}/disable", permFundAdjust, s.gapWrite(futures, http.MethodPost, "/internal/admin/mirrors/{id}/disable", "futures", "position_mirror_disable"))
}