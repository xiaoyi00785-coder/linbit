package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// registerColdSweepRoutes exposes manual hot→cold transfers under wallet config.
// Reads use withdraw:view (same as hot-wallet balances). Initiate / approve /
// reject / cancel require wallet:cold_sweep, 2FA and a reason, and are audited
// at high level. The destination is never taken from the client.
func (s *Server) registerColdSweepRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	wallet := func() string { return s.cfg.WalletURL }
	a("GET /admin/v1/wallet/cold-transfers/quote", permWithdrawView, s.gapRead(wallet, "/internal/admin/wallet/cold-transfers/quote"))
	a("GET /admin/v1/wallet/cold-transfers", permWithdrawView, s.gapRead(wallet, "/internal/admin/wallet/cold-transfers"))
	a("GET /admin/v1/wallet/cold-transfers/{id}", permWithdrawView, s.handleColdSweepGet)
	a("POST /admin/v1/wallet/cold-transfers", permWalletColdSweep, s.handleColdSweepCreate)
	a("POST /admin/v1/wallet/cold-transfers/{id}/approve", permWalletColdSweep, s.handleColdSweepDecide(true))
	a("POST /admin/v1/wallet/cold-transfers/{id}/reject", permWalletColdSweep, s.handleColdSweepDecide(false))
	a("POST /admin/v1/wallet/cold-transfers/{id}/cancel", permWalletColdSweep, s.handleColdSweepCancel)
}

func (s *Server) handleColdSweepGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.WalletURL, "/internal/admin/wallet/cold-transfers/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		upstreamErr(w, "cold-transfer get", err)
		return
	}
	relay(w, u)
}

func (s *Server) handleColdSweepCreate(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	m, code, reason, err := writeReq(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写操作原因")
		return
	}
	if !s.verify2FA(w, a, code) {
		return
	}
	stripColdDest(m)
	m["reason"] = strings.TrimSpace(reason)
	m["initiatedBy"] = a.ID
	m["initiatedByName"] = a.Username
	if key, _ := m["idempotencyKey"].(string); strings.TrimSpace(key) == "" {
		m["idempotencyKey"] = "admin-" + strconv.FormatInt(a.ID, 10) + "-" + a.reqID
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.WalletURL, "/internal/admin/wallet/cold-transfers", m)
	if err != nil {
		upstreamErr(w, "cold-transfer create", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	target := "wallet:" + strVal(m["asset"]) + "/" + strVal(m["network"])
	s.audit(r, a, "wallet", "cold_sweep_create", target, levelHigh, result, nil, json.RawMessage(u.Body), reason)
	relay(w, u)
}

func (s *Server) handleColdSweepDecide(approve bool) http.HandlerFunc {
	action := "cold_sweep_reject"
	if approve {
		action = "cold_sweep_approve"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id, ok := pathID(r)
		if !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		_, code, reason, err := writeReq(r)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
		if !approve && strings.TrimSpace(reason) == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写驳回原因")
			return
		}
		if !s.verify2FA(w, a, code) {
			return
		}
		path := "/internal/admin/wallet/cold-transfers/" + strconv.FormatInt(id, 10)
		if approve {
			path += "/approve"
		} else {
			path += "/reject"
		}
		body := map[string]any{"actorId": a.ID, "actorName": a.Username, "reason": strings.TrimSpace(reason)}
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.WalletURL, path, body)
		if err != nil {
			upstreamErr(w, action, err)
			return
		}
		result := "ok"
		if !u.ok() {
			result = "failed:" + u.errCode()
		}
		s.audit(r, a, "wallet", action, "cold-transfer:"+strconv.FormatInt(id, 10), levelHigh, result, nil, json.RawMessage(u.Body), reason)
		relay(w, u)
	}
}

func (s *Server) handleColdSweepCancel(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	_, code, reason, err := writeReq(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if !s.verify2FA(w, a, code) {
		return
	}
	body := map[string]any{"actorId": a.ID, "actorName": a.Username, "reason": strings.TrimSpace(reason)}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.WalletURL,
		"/internal/admin/wallet/cold-transfers/"+strconv.FormatInt(id, 10)+"/cancel", body)
	if err != nil {
		upstreamErr(w, "cold-transfer cancel", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	s.audit(r, a, "wallet", "cold_sweep_cancel", "cold-transfer:"+strconv.FormatInt(id, 10), levelHigh, result, nil, json.RawMessage(u.Body), reason)
	relay(w, u)
}

func strVal(v any) string {
	s, _ := v.(string)
	return s
}

func stripColdDest(m map[string]any) {
	if m == nil {
		return
	}
	for _, k := range []string{"destination", "to", "toAddress", "cold", "address", "target"} {
		delete(m, k)
	}
}
