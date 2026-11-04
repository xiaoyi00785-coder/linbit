package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// support.go: 站内客服工单 back office (tickets live in the account service).
// support:view lists/reads tickets and attachments; support:reply replies,
// changes status and assigns. Replies notify the user (account service). No
// 2FA on replies (routine CS work); every write is audited.

// GET /admin/v1/support/tickets?status=&category=&uid=&assigneeId=(id|none|me)&page=&pageSize=
func (s *Server) handleSupportTickets(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"status", "category", "uid", "assigneeId", "page", "pageSize"} {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			q.Set(k, v)
		}
	}
	if q.Get("assigneeId") == "me" {
		q.Set("assigneeId", jsonInt(actorOf(r).ID))
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/support/tickets?"+q.Encode(), nil)
	if err != nil {
		upstreamErr(w, "support tickets", err)
		return
	}
	relay(w, u)
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

// GET /admin/v1/support/tickets/{id}
func (s *Server) handleSupportTicket(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/support/tickets/"+url.PathEscape(r.PathValue("id")), nil)
	if err != nil {
		upstreamErr(w, "support ticket", err)
		return
	}
	relay(w, u)
}

// GET /admin/v1/support/attachments/{id} (image bytes)
func (s *Server) handleSupportAttachment(w http.ResponseWriter, r *http.Request) {
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/support/attachments/"+url.PathEscape(r.PathValue("id")), nil)
	if err != nil {
		upstreamErr(w, "support attachment", err)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	relay(w, u)
}

// POST /admin/v1/support/tickets/{id}/messages {content, status?: replied|closed}
func (s *Server) handleSupportReply(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id := r.PathValue("id")
	var req struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Content) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "回复内容必填")
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.AccountURL, "/internal/admin/support/tickets/"+url.PathEscape(id)+"/messages",
		map[string]any{"content": req.Content, "status": req.Status, "adminId": a.ID, "adminName": a.Username})
	if err != nil {
		upstreamErr(w, "support reply", err)
		return
	}
	result := "ok"
	if !u.ok() {
		result = "failed:" + u.errCode()
	}
	s.audit(r, a, "support", "reply", "ticket:"+id, levelNormal, result, nil, map[string]string{"status": firstNonEmpty(req.Status, "replied")}, "")
	relay(w, u)
}

// GET /admin/v1/support/assignees: active admins who can reply to tickets.
func (s *Server) handleSupportAssignees(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, username, roles FROM admin_users WHERE status='active' ORDER BY username`)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "查询失败")
		return
	}
	defer rows.Close()
	type item struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var roles []string
		if err := rows.Scan(&it.ID, &it.Username, (*textArray)(&roles)); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "查询失败")
			return
		}
		if can(roles, permSupportReply) {
			out = append(out, it)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// PATCH /admin/v1/support/tickets/{id} {status?, assigneeId?} (assigneeId 0 = unassign)
func (s *Server) handleSupportPatch(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id := r.PathValue("id")
	var req struct {
		Status     *string `json:"status"`
		AssigneeID *int64  `json:"assigneeId"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || (req.Status == nil && req.AssigneeID == nil) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请指定状态或处理人")
		return
	}
	body := map[string]any{"adminName": a.Username}
	if req.Status != nil {
		body["status"] = *req.Status
	}
	if req.AssigneeID != nil {
		body["assigneeId"] = *req.AssigneeID
		if *req.AssigneeID > 0 {
			var name string
			var roles []string
			err := s.db.QueryRowContext(r.Context(), `SELECT username, roles FROM admin_users WHERE id=$1 AND status='active'`, *req.AssigneeID).Scan(&name, (*textArray)(&roles))
			if err == sql.ErrNoRows || (err == nil && !can(roles, permSupportReply)) {
				common.WriteError(w, http.StatusBadRequest, "INVALID_ASSIGNEE", "处理人不存在、已停用或无工单回复权限")
				return
			}
			if err != nil {
				common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "查询失败")
				return
			}
			body["assigneeName"] = name
		}
	}
	u, err := s.call(r.Context(), http.MethodPatch, s.cfg.AccountURL, "/internal/admin/support/tickets/"+url.PathEscape(id), body)
	if err != nil {
		upstreamErr(w, "support update", err)
		return
	}
	result := "ok"
	var before any
	if u.ok() {
		var env struct {
			Before any `json:"before"`
		}
		_ = json.Unmarshal(u.Body, &env)
		before = env.Before
	} else {
		result = "failed:" + u.errCode()
	}
	s.audit(r, a, "support", "update", "ticket:"+id, levelNormal, result, before, body, "")
	relay(w, u)
}
