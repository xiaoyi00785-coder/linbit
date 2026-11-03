package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// notifications.go: operators push messages into users' message centers.
// Broadcasts (in-app only) and single-user sends (in-app + email per the
// user's preferences, or forced) need content:edit, a fresh 2FA code, and are
// audited.

type notifyReq struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Email    *bool  `json:"email,omitempty"`
	TOTPCode string `json:"totpCode"`
}

// POST /admin/v1/notifications/broadcast {kind, title, body, totpCode}
func (s *Server) handleNotifyBroadcast(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req notifyReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	body := map[string]string{"kind": req.Kind, "title": req.Title, "body": req.Body}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.AccountURL, "/internal/notifications/broadcast", body)
	if err != nil {
		upstreamErr(w, "notify broadcast", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	s.audit(r, a, "notification", "broadcast", "all", levelHigh, result, nil, body, "")
	relay(w, u)
}

// POST /admin/v1/users/{uid}/notifications {kind, title, body, email?, totpCode}
func (s *Server) handleNotifyUser(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	uid := strings.TrimSpace(r.PathValue("uid"))
	var req notifyReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	n := common.Notice{UID: uid, Kind: req.Kind, Title: req.Title, Body: req.Body, Email: req.Email}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.AccountURL, "/internal/notifications", n)
	if err != nil {
		upstreamErr(w, "notify user", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	s.audit(r, a, "notification", "send", "user:"+url.PathEscape(uid), levelNormal, result, nil, n, "")
	relay(w, u)
}
