package main

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/linbit/platform/services/go/common"
)

// GET /admin/v1/points/campaigns
func (s *Server) handlePointsCampaigns(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.GrowthURL, "/internal/points/campaigns", nil)
	if err != nil {
		upstreamErr(w, "points campaigns", err)
		return
	}
	relay(w, u)
}

// POST /admin/v1/points/campaigns {key, title, startsAt, endsAt, rules?, totpCode}
// Schedules a points carnival period (2FA, high-level audit).
func (s *Server) handlePointsCampaignCreate(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Key      string          `json:"key"`
		Title    string          `json:"title"`
		StartsAt int64           `json:"startsAt"`
		EndsAt   int64           `json:"endsAt"`
		Rules    json.RawMessage `json:"rules,omitempty"`
		TOTPCode string          `json:"totpCode"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	body := map[string]any{"key": req.Key, "title": req.Title, "startsAt": req.StartsAt, "endsAt": req.EndsAt, "operator": a.Username}
	if len(req.Rules) > 0 && string(req.Rules) != "null" {
		body["rules"] = req.Rules
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.GrowthURL, "/internal/points/campaigns", body)
	if err != nil {
		upstreamErr(w, "points campaign create", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	s.audit(r, a, "points", "campaign_create", "campaign:"+req.Key, levelHigh, result, nil, statusOf(u), "")
	relay(w, u)
}

// GET /admin/v1/users/{uid}/coupons — a user's vouchers (support view).
func (s *Server) handleUserCoupons(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.GrowthURL, "/internal/coupons?userId="+url.QueryEscape(r.PathValue("uid")), nil)
	if err != nil {
		upstreamErr(w, "user coupons", err)
		return
	}
	relay(w, u)
}
