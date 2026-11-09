package main

import (
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

func (s *Server) registerCardRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/cards/product", permContentEdit, s.cardCall(http.MethodGet, "/internal/cards/product", "card_read"))
	a("PUT /admin/v1/cards/product", permContentEdit, s.cardCall(http.MethodPut, "/internal/cards/product", "card_draft"))
	a("POST /admin/v1/cards/product/submit", permContentEdit, s.cardCall(http.MethodPost, "/internal/cards/product/submit", "card_submit"))
	a("POST /admin/v1/cards/product/approve", permContentEdit, s.cardCall(http.MethodPost, "/internal/cards/product/approve", "card_approve"))
	a("POST /admin/v1/cards/product/reject", permContentEdit, s.cardCall(http.MethodPost, "/internal/cards/product/reject", "card_reject"))
}

func (s *Server) cardCall(method, path, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if method == http.MethodGet {
			u, err := s.call(r.Context(), method, s.cfg.AccountURL, path, nil)
			if err != nil {
				upstreamErr(w, "cards", err)
				return
			}
			relay(w, u)
			return
		}
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
		if body == nil {
			body = map[string]any{}
		}
		body["updatedBy"] = a.Username
		if note, _ := body["note"].(string); strings.TrimSpace(note) == "" {
			body["note"] = reason
		}
		u, err := s.call(r.Context(), method, s.cfg.AccountURL, path, body)
		if err != nil {
			upstreamErr(w, "cards", err)
			return
		}
		s.audit(r, a, "cards", action, "card:product", levelHigh, resultOf(u), nil, nil, reason)
		relay(w, u)
	}
}
