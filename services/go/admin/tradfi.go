package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// tradfi.go: tokenized stocks & metals (market-data service). Reads need
// symbol:view; writes need symbol:edit + fresh 2FA + reason, audited (high).

var tfAssetRe = regexp.MustCompile(`^[A-Z0-9]{2,12}$`)

func (s *Server) registerTradFiRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/tradfi/assets", permSymbolView, s.tradfiGet("/internal/tradfi/assets"))
	a("GET /admin/v1/tradfi/settings", permSymbolView, s.tradfiGet("/internal/tradfi/settings"))
	a("GET /admin/v1/tradfi/deviation", permSymbolView, s.tradfiGet("/internal/tradfi/deviation"))
	a("PUT /admin/v1/tradfi/assets/{asset}", permSymbolEdit, s.tradfiWrite("tradfi_asset"))
	a("PUT /admin/v1/tradfi/tokens", permSymbolEdit, s.tradfiWrite("tradfi_token"))
	a("PUT /admin/v1/tradfi/settings", permSymbolEdit, s.tradfiWrite("tradfi_settings"))
}

func (s *Server) tradfiGet(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MarketURL, path, nil)
		if err != nil {
			upstreamErr(w, "tradfi", err)
			return
		}
		relay(w, u)
	}
}

func (s *Server) tradfiWrite(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		body, code, reason, err := writeReq(r)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
		if reason = strings.TrimSpace(reason); reason == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写操作原因")
			return
		}
		var path, target string
		switch kind {
		case "tradfi_asset":
			as := strings.ToUpper(strings.TrimSpace(r.PathValue("asset")))
			if !tfAssetRe.MatchString(as) {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "asset 不合法")
				return
			}
			path, target = "/internal/tradfi/assets/"+url.PathEscape(as), "tradfi_asset:"+as
		case "tradfi_token":
			as, _ := body["asset"].(string)
			is, _ := body["issuer"].(string)
			nw, _ := body["network"].(string)
			path, target = "/internal/tradfi/tokens", "tradfi_token:"+strings.ToUpper(as)+"/"+strings.ToLower(is)+"/"+strings.ToUpper(nw)
		default:
			path, target = "/internal/tradfi/settings", "tradfi_settings"
		}
		if !s.verify2FA(w, a, code) {
			return
		}
		body["updatedBy"] = a.Username
		u, err := s.call(r.Context(), http.MethodPut, s.cfg.MarketURL, path, body)
		if err != nil {
			upstreamErr(w, "tradfi "+kind, err)
			return
		}
		var ba struct {
			Before json.RawMessage `json:"before"`
			After  json.RawMessage `json:"after"`
		}
		var before, after any
		if u.ok() {
			_ = json.Unmarshal(u.Body, &ba)
			if len(ba.Before) > 0 && string(ba.Before) != "null" {
				before = ba.Before
			}
			if len(ba.After) > 0 && string(ba.After) != "null" {
				after = ba.After
			}
		}
		s.audit(r, a, "tradfi", kind, target, levelHigh, resultOf(u), before, after, reason)
		relay(w, u)
	}
}
