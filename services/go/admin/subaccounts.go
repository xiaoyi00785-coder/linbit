package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// Sub-account oversight: list (filter by parentUid/status/q) and global
// settings. Freezing a sub reuses the user freeze flow (2FA + reason + audit).

func (s *Server) handleListSubAccounts(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/sub-accounts"+passQuery(r, "parentUid", "status", "q", "page", "size"), nil)
	if err != nil {
		upstreamErr(w, "list sub-accounts", err)
		return
	}
	relay(w, u)
}

func (s *Server) handleGetSubSettings(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/sub-accounts/settings", nil)
	if err != nil {
		upstreamErr(w, "sub-account settings", err)
		return
	}
	relay(w, u)
}

type subSettingsReq struct {
	Enabled          bool   `json:"enabled"`
	MaxPerParent     int    `json:"maxPerParent"`
	RequireParent2FA bool   `json:"requireParent2fa"`
	AllowSubLogin    bool   `json:"allowSubLogin"`
	MaxAPIKeysPerSub int    `json:"maxApiKeysPerSub"`
	TOTPCode         string `json:"totpCode"`
	Reason           string `json:"reason"`
}

func (s *Server) handlePutSubSettings(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req subSettingsReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "参数不合法")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	before, _ := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/sub-accounts/settings", nil)
	body := map[string]any{"enabled": req.Enabled, "maxPerParent": req.MaxPerParent, "requireParent2fa": req.RequireParent2FA,
		"allowSubLogin": req.AllowSubLogin, "maxApiKeysPerSub": req.MaxAPIKeysPerSub}
	u, err := s.call(r.Context(), http.MethodPut, s.cfg.AccountURL, "/internal/admin/sub-accounts/settings", body)
	if err != nil {
		upstreamErr(w, "update sub-account settings", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	var prev any
	if before.ok() {
		prev = string(before.Body)
	}
	s.audit(r, a, "sub_account", "settings_update", "sub_account_settings", levelHigh, result, prev, body, req.Reason)
	relay(w, u)
}
