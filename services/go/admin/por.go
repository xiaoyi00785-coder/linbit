package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// por.go: proof-of-reserves snapshots, entered manually by admin and served
// publicly by market-data (GET /market/proof-of-reserves = latest snapshot).
// Reads need content:edit; writes also need a fresh 2FA code and a reason,
// and are audited (high) with before/after.
func (s *Server) registerPorRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/proof-of-reserves", permContentEdit, func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MarketURL, "/internal/por/snapshots"+passQuery(r, "limit"), nil)
		if err != nil {
			upstreamErr(w, "por", err)
			return
		}
		relay(w, u)
	})
	a("POST /admin/v1/proof-of-reserves", permContentEdit, s.porWrite(http.MethodPost))
	a("DELETE /admin/v1/proof-of-reserves/{id}", permContentEdit, s.porWrite(http.MethodDelete))
}

func (s *Server) porWrite(method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		if !s.verify2FA(w, a, code) {
			return
		}
		path, action, target := "/internal/por/snapshots", "por_create", "por:new"
		var send any
		if method == http.MethodDelete {
			id := r.PathValue("id")
			if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
				return
			}
			path, action, target = path+"/"+id, "por_delete", "por:"+id
		} else {
			body["updatedBy"] = a.Username
			send = body
		}
		u, err := s.call(r.Context(), method, s.cfg.MarketURL, path, send)
		if err != nil {
			upstreamErr(w, "por "+action, err)
			return
		}
		var ba struct {
			Before json.RawMessage `json:"before"`
			After  json.RawMessage `json:"after"`
		}
		var before, after any
		if u.ok() {
			_ = json.Unmarshal(u.Body, &ba)
			if len(ba.Before) > 0 {
				before = ba.Before
			}
			if len(ba.After) > 0 {
				after = ba.After
			}
		}
		s.audit(r, a, "por", action, target, levelHigh, resultOf(u), before, after, reason)
		relay(w, u)
	}
}
