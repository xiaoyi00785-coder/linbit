package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// partnerreview.go: manual review of partner (agent) applications in the
// growth service. Decisions need partner:review, a fresh 2FA code and, for
// reject / disable, a reason; every decision is audited.

// GET /admin/v1/partners?status=pending|active|rejected|disabled&page=&pageSize=
func (s *Server) handlePartnerApplications(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"status", "page", "pageSize"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.GrowthURL, "/internal/partners?"+q.Encode(), nil)
	if err != nil {
		upstreamErr(w, "partner applications", err)
		return
	}
	relay(w, u)
}

// POST /admin/v1/partners/{uid}/approve|reject|disable {reason, totpCode}
func (s *Server) handlePartnerReview(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		uid := r.PathValue("uid")
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		reason := strings.TrimSpace(firstNonEmpty(req.Reason, req.Comment))
		if decision != "approve" && reason == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回/停用必须填写原因")
			return
		}
		if !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.GrowthURL, "/internal/partners/"+url.PathEscape(uid)+"/review",
			map[string]string{"decision": decision, "reason": reason, "reviewer": a.Username})
		if err != nil {
			upstreamErr(w, "partner review", err)
			return
		}
		result := "ok"
		if !u.ok() {
			result = "failed:" + u.errCode()
		}
		s.audit(r, a, "partner", decision, "partner:"+uid, levelHigh, result, nil, statusOf(u), reason)
		relay(w, u)
	}
}
