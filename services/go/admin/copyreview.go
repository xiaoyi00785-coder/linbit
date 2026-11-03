package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// copyreview.go: manual review of copy-trading trader applications
// (FUTURES_COPY_AUTO_APPROVE=false). Decisions need copy:review, a fresh 2FA
// code and, for rejections, a reason; every decision is audited.

// GET /admin/v1/copy/applications?status=pending|active|rejected|closed|all&page=&pageSize=
func (s *Server) handleCopyApplications(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"status", "page", "pageSize"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.FuturesURL, "/internal/copy/applications?"+q.Encode(), nil)
	if err != nil {
		upstreamErr(w, "copy applications", err)
		return
	}
	relay(w, u)
}

// POST /admin/v1/copy/applications/{uid}/approve|reject {reason, totpCode}
func (s *Server) handleCopyReview(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		uid := r.PathValue("uid")
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
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.FuturesURL, "/internal/copy/applications/"+url.PathEscape(uid)+"/review",
			map[string]string{"decision": decision, "reason": reason, "reviewer": a.Username})
		if err != nil {
			upstreamErr(w, "copy review", err)
			return
		}
		result := "ok"
		if !u.ok() {
			result = "failed:" + u.errCode()
		}
		s.audit(r, a, "copy", decision, "trader:"+uid, levelHigh, result, nil, statusOf(u), reason)
		relay(w, u)
	}
}
