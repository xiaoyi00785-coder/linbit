package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// chat.go: trading chat moderation (market-data service). Reads need
// content:edit; every write needs content:edit, a fresh 2FA code and a
// non-empty reason, and is audited (high) with before/after.

func (s *Server) registerChatRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/chat/config", permContentEdit, s.chatGet("/internal/chat/config"))
	a("PUT /admin/v1/chat/config", permContentEdit, s.chatWrite(http.MethodPut, "chat_config"))
	a("GET /admin/v1/chat/messages", permContentEdit, s.chatGet("/internal/chat/messages", "room", "uid", "status", "q", "before", "limit"))
	a("DELETE /admin/v1/chat/messages/{id}", permContentEdit, s.chatWrite(http.MethodDelete, "chat_delete_message"))
	a("GET /admin/v1/chat/sanctions", permContentEdit, s.chatGet("/internal/chat/sanctions"))
	a("POST /admin/v1/chat/mutes", permContentEdit, s.chatWrite(http.MethodPost, "chat_mute"))
	a("DELETE /admin/v1/chat/mutes/{uid}", permContentEdit, s.chatWrite(http.MethodDelete, "chat_unmute"))
	a("POST /admin/v1/chat/bans", permContentEdit, s.chatWrite(http.MethodPost, "chat_ban"))
	a("DELETE /admin/v1/chat/bans/{uid}", permContentEdit, s.chatWrite(http.MethodDelete, "chat_unban"))
	a("GET /admin/v1/chat/reports", permContentEdit, s.chatGet("/internal/chat/reports", "status", "limit"))
	a("POST /admin/v1/chat/reports/{id}/resolve", permContentEdit, s.chatWrite(http.MethodPost, "chat_resolve_report"))
}

func (s *Server) chatGet(path string, keys ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MarketURL, path+passQuery(r, keys...), nil)
		if err != nil {
			upstreamErr(w, "chat", err)
			return
		}
		relay(w, u)
	}
}

func (s *Server) chatWrite(method, action string) http.HandlerFunc {
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
		id, uid := r.PathValue("id"), strings.TrimSpace(r.PathValue("uid"))
		if id != "" {
			if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
				return
			}
		}
		if uid == "" {
			uid, _ = body["uid"].(string)
		}
		var path, target string
		switch action {
		case "chat_config":
			path, target = "/internal/chat/config", "chat_config"
		case "chat_delete_message":
			path, target = "/internal/chat/messages/"+id, "chat_message:"+id
		case "chat_mute":
			path, target = "/internal/chat/mutes", "user:"+uid
		case "chat_unmute":
			path, target = "/internal/chat/mutes/"+url.PathEscape(uid), "user:"+uid
		case "chat_ban":
			path, target = "/internal/chat/bans", "user:"+uid
		case "chat_unban":
			path, target = "/internal/chat/bans/"+url.PathEscape(uid), "user:"+uid
		case "chat_resolve_report":
			path, target = "/internal/chat/reports/"+id+"/resolve", "chat_report:"+id
		}
		delete(body, "updatedBy")
		body["updatedBy"] = a.Username
		if action != "chat_config" {
			body["reason"] = reason // stored on the sanction / deletion record
		}
		u, err := s.call(r.Context(), method, s.cfg.MarketURL, path, body)
		if err != nil {
			upstreamErr(w, "chat "+action, err)
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
		s.audit(r, a, "chat", action, target, levelHigh, resultOf(u), before, after, reason)
		relay(w, u)
	}
}
