package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// ---- users ----

func maskEmail(e string) string { return common.MaskEmail(e) }

func maskPhone(p string) string { return common.MaskPhone(p) }

func maskUser(m map[string]interface{}) {
	if e, ok := m["email"].(string); ok && e != "" {
		m["email"] = maskEmail(e)
	}
	if p, ok := m["phone"].(string); ok && p != "" {
		m["phone"] = maskPhone(p)
	}
}

// wantsReveal reports whether the caller asked for unmasked PII and may see it.
func wantsReveal(r *http.Request, a *actor) (reveal, denied bool) {
	if r.URL.Query().Get("reveal") != "true" {
		return false, false
	}
	return can(a.Roles, permUserPII), !can(a.Roles, permUserPII)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	reveal, denied := wantsReveal(r, a)
	if denied {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "无查看完整个人信息权限")
		return
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/users"+passQuery(r, "q", "status", "page", "size"), nil)
	if err != nil {
		upstreamErr(w, "list users", err)
		return
	}
	if !u.ok() {
		relay(w, u)
		return
	}
	var body map[string]interface{}
	if err := u.decode(&body); err != nil {
		upstreamErr(w, "list users decode", err)
		return
	}
	if items, ok := body["items"].([]interface{}); ok && !reveal {
		for _, it := range items {
			if m, ok := it.(map[string]interface{}); ok {
				maskUser(m)
			}
		}
	}
	if reveal {
		s.audit(r, a, "user", "reveal_pii_list", r.URL.Query().Get("q"), levelSensitive, "ok", nil, nil, "")
	}
	common.WriteJSON(w, http.StatusOK, body)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	reveal, denied := wantsReveal(r, a)
	if denied {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "无查看完整个人信息权限")
		return
	}
	uid := r.PathValue("uid")
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/users/"+url.PathEscape(uid), nil)
	if err != nil {
		upstreamErr(w, "get user", err)
		return
	}
	if !u.ok() {
		relay(w, u)
		return
	}
	var m map[string]interface{}
	if err := u.decode(&m); err != nil {
		upstreamErr(w, "get user decode", err)
		return
	}
	if reveal {
		s.audit(r, a, "user", "reveal_pii", "user:"+uid, levelSensitive, "ok", nil, nil, "")
	} else {
		maskUser(m)
	}
	common.WriteJSON(w, http.StatusOK, m)
}

type reasonReq struct {
	Reason   string `json:"reason"`
	Comment  string `json:"comment"`
	TOTPCode string `json:"totpCode"`
}

// handleFreeze freezes / unfreezes a user account (2FA + reason; high-risk audit).
func (s *Server) handleFreeze(freeze bool) http.HandlerFunc {
	action, status := "unfreeze", "active"
	if freeze {
		action, status = "freeze", "frozen"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		uid := r.PathValue("uid")
		var req reasonReq
		if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
			return
		}
		if !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		before, _ := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/users/"+url.PathEscape(uid), nil)
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.AccountURL, "/internal/admin/users/"+url.PathEscape(uid)+"/status",
			map[string]string{"status": status, "reason": req.Reason})
		if err != nil {
			upstreamErr(w, action, err)
			return
		}
		result := "ok"
		if !u.ok() {
			result = "failed:" + u.errCode()
		}
		s.audit(r, a, "user", action, "user:"+uid, levelHigh, result, statusOf(before), statusOf(u), req.Reason)
		relay(w, u)
	}
}

func statusOf(u upstream) interface{} {
	var m struct {
		Status       string `json:"status"`
		StatusReason string `json:"statusReason"`
	}
	if !u.ok() || json.Unmarshal(u.Body, &m) != nil {
		return nil
	}
	return m
}

func (s *Server) handleUserBalances(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AssetURL, "/internal/balances?userId="+url.QueryEscape(r.PathValue("uid")), nil)
	if err != nil {
		upstreamErr(w, "balances", err)
		return
	}
	relay(w, u)
}

func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AssetURL, "/internal/admin/ledger"+passQuery(r, "userId", "asset", "txType", "ref", "page", "size"), nil)
	if err != nil {
		upstreamErr(w, "ledger", err)
		return
	}
	relay(w, u)
}

// ---- KYC ----

func (s *Server) handleKYCList(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.KYCURL, "/admin/kyc/applications"+passQuery(r, "status"), nil)
	if err != nil {
		upstreamErr(w, "kyc list", err)
		return
	}
	relay(w, u)
}

func (s *Server) handleKYCGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.KYCURL, "/admin/kyc/applications/"+url.PathEscape(id), nil)
	if err != nil {
		upstreamErr(w, "kyc get", err)
		return
	}
	if u.ok() {
		s.audit(r, actorOf(r), "kyc", "view", "kyc:"+id, levelSensitive, "ok", nil, nil, "")
	}
	relay(w, u)
}

func (s *Server) handleKYCDecision(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id := r.PathValue("id")
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		if decision == "reject" && strings.TrimSpace(req.Reason) == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
			return
		}
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.KYCURL, "/admin/kyc/applications/"+url.PathEscape(id)+"/"+decision,
			map[string]string{"reason": req.Reason})
		if err != nil {
			upstreamErr(w, "kyc "+decision, err)
			return
		}
		result := "ok"
		if !u.ok() {
			result = "failed:" + u.errCode()
		}
		s.audit(r, a, "kyc", decision, "kyc:"+id, levelSensitive, result, nil, json.RawMessage(nil), firstNonEmpty(req.Reason, req.Comment))
		relay(w, u)
	}
}

func (s *Server) handleKYCDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.KYCURL, "/admin/kyc/documents/"+url.PathEscape(id), nil)
	if err != nil {
		upstreamErr(w, "kyc doc", err)
		return
	}
	s.audit(r, actorOf(r), "kyc", "view_document", "kyc_doc:"+id, levelSensitive, map[bool]string{true: "ok", false: "failed"}[u.ok()], nil, nil, "")
	w.Header().Set("Cache-Control", "no-store")
	relay(w, u)
}

// ---- symbols ----

func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.OrderURL, "/internal/admin/symbols", nil)
	if err != nil {
		upstreamErr(w, "symbols", err)
		return
	}
	relay(w, u)
}

func (s *Server) handleCreateSymbol(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.OrderURL, "/internal/admin/symbols", body)
	if err != nil {
		upstreamErr(w, "create symbol", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	s.audit(r, actorOf(r), "symbol", "create", symbolTarget(body), levelHigh, result, nil, body, "")
	relay(w, u)
}

func symbolTarget(b map[string]interface{}) string {
	base, _ := b["base"].(string)
	quote, _ := b["quote"].(string)
	return "symbol:" + strings.ToUpper(base+quote)
}

func (s *Server) handlePatchSymbol(w http.ResponseWriter, r *http.Request) {
	sym := r.PathValue("symbol")
	var body map[string]interface{}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	reason, _ := body["reason"].(string)
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	delete(body, "reason")
	var before interface{}
	if l, err := s.call(r.Context(), http.MethodGet, s.cfg.OrderURL, "/internal/admin/symbols", nil); err == nil && l.ok() {
		var list struct {
			Items []map[string]interface{} `json:"items"`
		}
		if l.decode(&list) == nil {
			for _, it := range list.Items {
				if v, _ := it["symbol"].(string); strings.EqualFold(v, sym) {
					before = it
				}
			}
		}
	}
	u, err := s.call(r.Context(), http.MethodPatch, s.cfg.OrderURL, "/internal/admin/symbols/"+url.PathEscape(sym), body)
	if err != nil {
		upstreamErr(w, "patch symbol", err)
		return
	}
	result := "ok"
	var after interface{}
	if !u.ok() {
		result = "failed:" + u.errCode()
	} else {
		var m map[string]interface{}
		if u.decode(&m) == nil {
			after = m
		}
	}
	s.audit(r, actorOf(r), "symbol", "update", "symbol:"+strings.ToUpper(sym), levelHigh, result, before, after, reason)
	relay(w, u)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
