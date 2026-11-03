package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// vip.go: manual review of VIP applications (account service). Listing needs
// vip:view; decisions need vip:review, a fresh 2FA code and, for rejections, a
// reason; every decision is audited. VIP has no fee tiers.

// GET /admin/v1/vip/applications?status=pending|approved|rejected|all&page=&pageSize=
func (s *Server) handleVIPApplications(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"status", "page", "pageSize"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/vip/applications?"+q.Encode(), nil)
	if err != nil {
		upstreamErr(w, "vip applications", err)
		return
	}
	relay(w, u)
}

// POST /admin/v1/vip/applications/{id}/approve|reject {reason, totpCode}
func (s *Server) handleVIPReview(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id := r.PathValue("id")
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		reason := strings.TrimSpace(firstNonEmpty(req.Reason, req.Comment))
		if decision == "reject" && reason == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
			return
		}
		if !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.AccountURL, "/internal/vip/applications/"+url.PathEscape(id)+"/review",
			map[string]string{"decision": decision, "reason": reason, "reviewer": a.Username})
		if err != nil {
			upstreamErr(w, "vip review", err)
			return
		}
		result := "ok"
		if !u.ok() {
			result = "failed:" + u.errCode()
		}
		s.audit(r, a, "vip", decision, "vip:"+id, levelHigh, result, nil, statusOf(u), reason)
		relay(w, u)
	}
}
