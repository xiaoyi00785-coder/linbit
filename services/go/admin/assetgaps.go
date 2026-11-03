package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Admin proxies for funding/spot settings, wallet network config, internal
// transfer config, and per-user spot/futures fee overrides. Reads relay as-is;
// writes require 2FA + a reason, stamp updatedBy, and are audited (high).

func (s *Server) assetGapRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	asset := func() string { return s.cfg.AssetURL }
	wallet := func() string { return s.cfg.WalletURL }
	order := func() string { return s.cfg.OrderURL }
	futures := func() string { return s.cfg.FuturesURL }

	a("GET /admin/v1/asset-settings", permLedgerView, s.gapRead(asset, "/internal/admin/asset-settings"))
	a("PUT /admin/v1/asset-settings", permSymbolEdit, s.gapWrite(asset, http.MethodPut, "/internal/admin/asset-settings", "asset", "settings_update"))

	a("GET /admin/v1/wallet/networks", permWithdrawView, s.gapRead(wallet, "/internal/admin/wallet/networks"))
	a("PUT /admin/v1/wallet/networks/{asset}/{network}", permSymbolEdit, s.gapWrite(wallet, http.MethodPut, "/internal/admin/wallet/networks/{asset}/{network}", "wallet", "network_update"))

	a("GET /admin/v1/internal-transfer/config", permWithdrawView, s.gapRead(wallet, "/internal/admin/internal-transfer/config"))
	a("PUT /admin/v1/internal-transfer/config", permSymbolEdit, s.gapWrite(wallet, http.MethodPut, "/internal/admin/internal-transfer/config", "wallet", "internal_transfer_config"))
	a("GET /admin/v1/internal-transfers", permLedgerView, s.gapRead(wallet, "/internal/admin/internal-transfers"))

	a("GET /admin/v1/fee-overrides/spot", permFeeView, s.gapRead(order, "/internal/admin/fee-overrides"))
	a("PUT /admin/v1/fee-overrides/spot/{userId}", permFeeEdit, s.gapWrite(order, http.MethodPut, "/internal/admin/fee-overrides/{userId}", "fee", "spot_user_override"))
	a("DELETE /admin/v1/fee-overrides/spot/{userId}", permFeeEdit, s.gapWrite(order, http.MethodDelete, "/internal/admin/fee-overrides/{userId}", "fee", "spot_user_override_delete"))
	a("GET /admin/v1/fee-overrides/futures", permFeeView, s.gapRead(futures, "/internal/admin/fee-overrides"))
	a("PUT /admin/v1/fee-overrides/futures/{userId}", permFeeEdit, s.gapWrite(futures, http.MethodPut, "/internal/admin/fee-overrides/{userId}", "fee", "futures_user_override"))
	a("DELETE /admin/v1/fee-overrides/futures/{userId}", permFeeEdit, s.gapWrite(futures, http.MethodDelete, "/internal/admin/fee-overrides/{userId}", "fee", "futures_user_override_delete"))
}

func fillPath(r *http.Request, p string) string {
	for _, k := range []string{"asset", "network", "userId"} {
		p = strings.ReplaceAll(p, "{"+k+"}", url.PathEscape(r.PathValue(k)))
	}
	return p
}

func (s *Server) gapRead(base func() string, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := fillPath(r, path)
		if q := r.URL.RawQuery; q != "" {
			p += "?" + q
		}
		u, err := s.call(r.Context(), http.MethodGet, base(), p, nil)
		if err != nil {
			upstreamErr(w, "proxy read", err)
			return
		}
		relay(w, u)
	}
}

func (s *Server) gapWrite(base func() string, method, path, module, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		m["updatedBy"] = a.Username
		if _, ok := m["note"]; !ok && strings.Contains(module+action, "override") {
			m["note"] = reason
		}
		p := fillPath(r, path)
		var body any = m
		if method == http.MethodDelete {
			body = nil
		}
		u, err := s.call(r.Context(), method, base(), p, body)
		if err != nil {
			upstreamErr(w, module+" "+action, err)
			return
		}
		result := "ok"
		var ba struct {
			Before json.RawMessage `json:"before"`
			After  json.RawMessage `json:"after"`
		}
		var before, after any
		if !u.ok() {
			result = "failed:" + u.errCode()
		} else if json.Unmarshal(u.Body, &ba) == nil && len(ba.After) > 0 {
			before, after = ba.Before, ba.After
		} else {
			after = json.RawMessage(u.Body)
		}
		s.audit(r, a, module, action, p, levelHigh, result, before, after, reason)
		relay(w, u)
	}
}
