package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

func (s *Server) registerAcademyRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/academy/categories", permContentEdit, s.academyGet("/internal/academy/categories"))
	a("POST /admin/v1/academy/categories", permContentEdit, s.academyWrite(http.MethodPost, "/internal/academy/categories", "academy_category"))
	a("PUT /admin/v1/academy/categories/{id}", permContentEdit, s.academyWrite(http.MethodPut, "/internal/academy/categories/", "academy_category"))
	a("DELETE /admin/v1/academy/categories/{id}", permContentEdit, s.academyWrite(http.MethodDelete, "/internal/academy/categories/", "academy_category_delete"))
	a("GET /admin/v1/academy/articles", permContentEdit, s.academyGet("/internal/academy/articles"))
	a("POST /admin/v1/academy/articles", permContentEdit, s.academyWrite(http.MethodPost, "/internal/academy/articles", "academy_article"))
	a("PUT /admin/v1/academy/articles/{id}", permContentEdit, s.academyWrite(http.MethodPut, "/internal/academy/articles/", "academy_article"))
	a("POST /admin/v1/academy/articles/{id}/publish", permContentEdit, s.academyWrite(http.MethodPost, "/internal/academy/articles/", "academy_publish"))
	a("POST /admin/v1/academy/articles/{id}/unpublish", permContentEdit, s.academyWrite(http.MethodPost, "/internal/academy/articles/", "academy_unpublish"))
}

func (s *Server) academyGet(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := path
		if q := r.URL.RawQuery; q != "" {
			p += "?" + q
		}
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MarketURL, p, nil)
		if err != nil {
			upstreamErr(w, "academy", err)
			return
		}
		relay(w, u)
	}
}

func (s *Server) academyWrite(method, path, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		body, code, reason, err := writeReq(r)
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
		full := path
		target := action
		if id := r.PathValue("id"); id != "" {
			if strings.Contains(path, "articles") {
				if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
					common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
					return
				}
			}
			full = strings.TrimRight(path, "/") + "/" + url.PathEscape(id)
			if action == "academy_publish" {
				full += "/publish"
			}
			if action == "academy_unpublish" {
				full += "/unpublish"
			}
			target = action + ":" + id
		}
		var send any
		if method != http.MethodDelete {
			if body == nil {
				body = map[string]any{}
			}
			body["updatedBy"] = a.Username
			send = body
		}
		u, err := s.call(r.Context(), method, s.cfg.MarketURL, full, send)
		if err != nil {
			upstreamErr(w, "academy", err)
			return
		}
		s.audit(r, a, "academy", action, target, levelHigh, resultOf(u), nil, nil, reason)
		relay(w, u)
	}
}
