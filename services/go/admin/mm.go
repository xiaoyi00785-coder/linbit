package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// mm.go: market-maker controls. Reads need mm:view; every write needs mm:edit,
// a fresh 2FA code and is audited (high). The virtual testnet seed is refused
// here as well as in marketmaker and asset whenever LINBIT_ENV=production or
// WALLET_CHAIN is not testnet/mock.

func (s *Server) handleMMGet(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MMURL, path, nil)
		if err != nil {
			upstreamErr(w, "mm", err)
			return
		}
		relay(w, u)
	}
}

func resultOf(u upstream) string {
	if u.ok() {
		return "ok"
	}
	return "failed:" + u.errCode()
}

// PUT /admin/v1/mm/pairs/{market}/{symbol} {<params...>, reason, totpCode}
func (s *Server) handleMMUpdatePair(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	market, sym := r.PathValue("market"), strings.ToUpper(r.PathValue("symbol"))
	if market != "spot" && market != "futures" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "market 必须为 spot/futures")
		return
	}
	var body map[string]json.RawMessage
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	var code, reason string
	_ = json.Unmarshal(body["totpCode"], &code)
	_ = json.Unmarshal(body["reason"], &reason)
	delete(body, "totpCode")
	delete(body, "reason")
	delete(body, "updatedBy")
	if !s.verify2FA(w, a, code) {
		return
	}
	body["updatedBy"], _ = json.Marshal(a.Username)
	u, err := s.call(r.Context(), http.MethodPut, s.cfg.MMURL, "/internal/mm/pairs/"+url.PathEscape(market)+"/"+url.PathEscape(sym), body)
	if err != nil {
		upstreamErr(w, "mm pair", err)
		return
	}
	delete(body, "updatedBy")
	s.audit(r, a, "mm", "update_pair", "mm:"+market+"/"+sym, levelHigh, resultOf(u), nil, body, reason)
	relay(w, u)
}

// POST /admin/v1/mm/start|stop {reason, totpCode}
func (s *Server) handleMMRun(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		if !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.MMURL, "/internal/mm/"+action, map[string]string{"by": a.Username})
		if err != nil {
			upstreamErr(w, "mm "+action, err)
			return
		}
		s.audit(r, a, "mm", action, "mm:"+s.cfg.MMUserID, levelHigh, resultOf(u), nil, nil, strings.TrimSpace(req.Reason))
		relay(w, u)
	}
}

// POST /admin/v1/mm/testnet-seed {ref, reason, totpCode} — testnet/mock only.
func (s *Server) handleMMSeed(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Ref      string `json:"ref"`
		Reason   string `json:"reason"`
		TOTPCode string `json:"totpCode"`
	}
	_ = common.DecodeJSON(r, &req)
	if ok, why := common.TestnetSeedAllowed(); !ok {
		s.audit(r, a, "mm", "testnet_seed", "mm:"+s.cfg.MMUserID, levelHigh, "refused:SEED_DISABLED", nil, nil, why)
		common.WriteError(w, http.StatusForbidden, "SEED_DISABLED", "虚拟注资已禁用："+why)
		return
	}
	req.Ref = strings.TrimSpace(req.Ref)
	if req.Ref == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "ref 必填（幂等键）")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.MMURL, "/internal/mm/seed", map[string]string{"ref": req.Ref})
	if err != nil {
		upstreamErr(w, "mm seed", err)
		return
	}
	s.audit(r, a, "mm", "testnet_seed", "mm:"+s.cfg.MMUserID, levelHigh, resultOf(u), nil,
		map[string]string{"ref": req.Ref, "txType": "mm_testnet_seed"}, req.Reason)
	relay(w, u)
}

// POST /admin/v1/mm/futures-transfer {direction in|out, amount, reason, totpCode}
func (s *Server) handleMMTransfer(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Direction string `json:"direction"`
		Amount    string `json:"amount"`
		Reason    string `json:"reason"`
		TOTPCode  string `json:"totpCode"`
	}
	_ = common.DecodeJSON(r, &req)
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.MMURL, "/internal/mm/futures-transfer",
		map[string]string{"direction": req.Direction, "amount": req.Amount})
	if err != nil {
		upstreamErr(w, "mm transfer", err)
		return
	}
	s.audit(r, a, "mm", "futures_transfer", "mm:"+s.cfg.MMUserID, levelHigh, resultOf(u), nil,
		map[string]string{"direction": req.Direction, "amount": req.Amount}, req.Reason)
	relay(w, u)
}
