package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// event.go: event contracts (futures service). Reads need symbol:view; every
// write needs symbol:edit, a fresh 2FA code and a non-empty reason, and is
// audited (high) with before/after.

var evSymbolRe = regexp.MustCompile(`^[A-Z0-9]{2,20}$`)

func (s *Server) registerEventRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/event/config", permSymbolView, s.eventGet("/internal/event/config"))
	a("PUT /admin/v1/event/config", permSymbolEdit, s.eventWrite("event_config"))
	a("PUT /admin/v1/event/symbols/{symbol}", permSymbolEdit, s.eventWrite("event_symbol"))
	a("GET /admin/v1/event/orders", permSymbolView, s.eventGet("/internal/event/orders", "uid", "symbol", "status", "before", "limit"))
	a("GET /admin/v1/event/exposure", permSymbolView, s.eventGet("/internal/event/exposure"))
}

func (s *Server) eventGet(path string, keys ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.FuturesURL, path+passQuery(r, keys...), nil)
		if err != nil {
			upstreamErr(w, "event", err)
			return
		}
		relay(w, u)
	}
}

func (s *Server) eventWrite(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		action := kind // per request: the closure is shared
		a := actorOf(r)
		body, code, reason, err := writeReq(r)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写操作原因")
			return
		}
		path, target := "/internal/event/config", "event_config"
		if action == "event_symbol" {
			sym := strings.ToUpper(strings.TrimSpace(r.PathValue("symbol")))
			if !evSymbolRe.MatchString(sym) {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "symbol 不合法")
				return
			}
			path, target = "/internal/event/symbols/"+url.PathEscape(sym), "event_symbol:"+sym
			if p, ok := body["paused"].(bool); ok {
				if p {
					action = "event_symbol_pause"
				} else {
					action = "event_symbol_resume"
				}
			}
		}
		if !s.verify2FA(w, a, code) {
			return
		}
		body["updatedBy"] = a.Username
		u, err := s.call(r.Context(), http.MethodPut, s.cfg.FuturesURL, path, body)
		if err != nil {
			upstreamErr(w, "event "+action, err)
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
		s.audit(r, a, "event", action, target, levelHigh, resultOf(u), before, after, reason)
		relay(w, u)
	}
}
