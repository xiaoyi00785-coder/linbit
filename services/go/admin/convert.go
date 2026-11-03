package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// convert.go: 闪兑 back-office (config, orders, platform position) proxied to
// the asset service.

func (s *Server) handleAssetGet(path string, keys []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := path
		if len(keys) > 0 {
			p += passQuery(r, keys...)
		}
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.AssetURL, p, nil)
		if err != nil {
			upstreamErr(w, "convert", err)
			return
		}
		relay(w, u)
	}
}

// PUT /admin/v1/convert/config {spreadBps?, minUsdt?, maxUsdt?, depegBps?,
// hedge?:{enabled?, slippageBps?, maxOrderUsdt?, maxHourUsdt?, unhedgedAlertUsdt?, unhedgedAlertMinutes?, cancelAfterSeconds?},
// assets?:[{asset, enabled?, hedgeEnabled?, hedgeMinQty?}], reason, totpCode}
// Every change (incl. the hedge switches) needs 2FA + a reason and is audited.
func (s *Server) handleConvertConfig(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
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
	if !s.verify2FA(w, a, code) {
		return
	}
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写变更原因 reason")
		return
	}
	before, _ := s.call(r.Context(), http.MethodGet, s.cfg.AssetURL, "/internal/convert/config", nil)
	body["updatedBy"], _ = json.Marshal(a.Username)
	u, err := s.call(r.Context(), http.MethodPut, s.cfg.AssetURL, "/internal/convert/config", body)
	if err != nil {
		upstreamErr(w, "convert config", err)
		return
	}
	delete(body, "updatedBy")
	s.audit(r, a, "convert", "update_config", "convert:config", levelHigh, resultOf(u), json.RawMessage(before.Body), body, reason)
	relay(w, u)
}
