package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// home.go: home-page operations slots (placements incl. banners, hot-list pins), stored by the
// market-data service. Reads need content:edit; every write needs content:edit,
// a fresh 2FA code and a non-empty reason, and is audited (high) with
// before/after.

func (s *Server) registerHomeRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/home/placements", permContentEdit, s.handleHomeGet("/internal/home/placements"))
	a("POST /admin/v1/home/placements", permContentEdit, s.handleHomeWrite(http.MethodPost, "create_placement"))
	a("PUT /admin/v1/home/placements/{id}", permContentEdit, s.handleHomeWrite(http.MethodPut, "update_placement"))
	a("DELETE /admin/v1/home/placements/{id}", permContentEdit, s.handleHomeWrite(http.MethodDelete, "delete_placement"))
	// Banners = placements with slot=carousel (compatible alias).
	a("GET /admin/v1/banners", permContentEdit, s.handleHomeGet("/internal/home/placements?slot=carousel"))
	a("POST /admin/v1/banners", permContentEdit, s.handleHomeWrite(http.MethodPost, "create_banner"))
	a("PUT /admin/v1/banners/{id}", permContentEdit, s.handleHomeWrite(http.MethodPut, "update_banner"))
	a("DELETE /admin/v1/banners/{id}", permContentEdit, s.handleHomeWrite(http.MethodDelete, "delete_banner"))
	a("GET /admin/v1/app/features", permContentEdit, s.handleHomeGet("/internal/app/features"))
	a("PUT /admin/v1/app/features", permContentEdit, s.handleHomeWrite(http.MethodPut, "set_app_features"))
	a("GET /admin/v1/home/hot-pins", permContentEdit, s.handleHomeGet("/internal/home/hot-pins"))
	a("PUT /admin/v1/home/hot-pins/{type}", permContentEdit, s.handleHomeWrite(http.MethodPut, "set_hot_pins"))
}

func (s *Server) handleHomeGet(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MarketURL, path, nil)
		if err != nil {
			upstreamErr(w, "home", err)
			return
		}
		relay(w, u)
	}
}

func (s *Server) handleHomeWrite(method, action string) http.HandlerFunc {
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
		var path, target string
		switch action {
		case "set_app_features":
			path, target = "/internal/app/features", "app_features"
		case "set_hot_pins":
			typ := strings.ToLower(r.PathValue("type"))
			path, target = "/internal/home/hot-pins/"+url.PathEscape(typ), "home_hot:"+typ
		default:
			path, target = "/internal/home/placements", "placement:new"
			if id := r.PathValue("id"); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
					common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
					return
				}
				path, target = path+"/"+id, "placement:"+id
			}
		}
		var payload any
		if method != http.MethodDelete {
			delete(body, "updatedBy")
			if strings.HasSuffix(action, "_banner") {
				body["slot"] = "carousel"
			}
			body["updatedBy"] = a.Username
			payload = body
		}
		u, err := s.call(r.Context(), method, s.cfg.MarketURL, path, payload)
		if err != nil {
			upstreamErr(w, "home "+action, err)
			return
		}
		var ba struct {
			Before    json.RawMessage `json:"before"`
			After     json.RawMessage `json:"after"`
			Placement struct {
				ID int64 `json:"id"`
			} `json:"placement"`
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
			if target == "placement:new" && ba.Placement.ID > 0 {
				target = "placement:" + strconv.FormatInt(ba.Placement.ID, 10)
			}
		}
		s.audit(r, a, "content", action, target, levelHigh, resultOf(u), before, after, reason)
		relay(w, u)
	}
}
