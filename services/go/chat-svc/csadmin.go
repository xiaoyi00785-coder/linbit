package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// csadmin.go: internal customer-service workbench, called only by the admin
// service (which enforces RBAC/2FA/audit). Guarded by X-Internal-Token.

type adminConv struct {
	ID          int64     `json:"id"`
	PubToken    string    `json:"pubToken"`
	UID         string    `json:"uid"`
	GuestKey    string    `json:"guestKey"`
	Subject     string    `json:"subject"`
	Status      string    `json:"status"`
	Assignee    string    `json:"assignee"`
	LastMsgAt   time.Time `json:"lastMsgAt"`
	UnreadStaff int       `json:"unreadStaff"`
	CreatedAt   time.Time `json:"createdAt"`
}

type adminCSMessage struct {
	ID         int64     `json:"id"`
	ConvID     int64     `json:"convId"`
	Sender     string    `json:"sender"`
	SenderKind string    `json:"senderKind"`
	Kind       string    `json:"kind"`
	Text       string    `json:"text"`
	ImageURL   string    `json:"imageUrl,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (c *CS) iList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := []string{"true"}, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if v := q.Get("status"); v != "" && v != "all" {
		add("status = ?", v)
	}
	if v := q.Get("assignee"); v != "" {
		add("assignee = ?", v)
	}
	if v := q.Get("uid"); v != "" {
		add("uid = ?", v)
	}
	limit := atoiDefault(q.Get("limit"), 50, 1, 200)
	rows, err := c.srv.store.db.QueryContext(r.Context(),
		`SELECT id, pub_token, uid, guest_key, subject, status, assignee, last_msg_at, unread_staff, created_at
		 FROM cs_conversations WHERE `+strings.Join(where, " AND ")+` ORDER BY last_msg_at DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		intErr(w, "cs admin list", err)
		return
	}
	defer rows.Close()
	items := []adminConv{}
	for rows.Next() {
		var a adminConv
		if err := rows.Scan(&a.ID, &a.PubToken, &a.UID, &a.GuestKey, &a.Subject, &a.Status, &a.Assignee, &a.LastMsgAt, &a.UnreadStaff, &a.CreatedAt); err != nil {
			intErr(w, "cs admin list", err)
			return
		}
		items = append(items, a)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (c *CS) iMessages(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	after := int64(atoiDefault(r.URL.Query().Get("after"), 0, 0, 1<<62))
	limit := atoiDefault(r.URL.Query().Get("limit"), 100, 1, 500)
	rows, err := c.srv.store.db.QueryContext(r.Context(),
		`SELECT id, conv_id, sender, sender_kind, kind, text, image_url, status, created_at
		 FROM cs_messages WHERE conv_id=$1 AND id > $2 ORDER BY id ASC LIMIT $3`, id, after, limit)
	if err != nil {
		intErr(w, "cs admin messages", err)
		return
	}
	defer rows.Close()
	items := []adminCSMessage{}
	for rows.Next() {
		var m adminCSMessage
		if err := rows.Scan(&m.ID, &m.ConvID, &m.Sender, &m.SenderKind, &m.Kind, &m.Text, &m.ImageURL, &m.Status, &m.CreatedAt); err != nil {
			intErr(w, "cs admin messages", err)
			return
		}
		items = append(items, m)
	}
	// Reading the queue clears the staff-side unread counter.
	_, _ = c.srv.store.db.ExecContext(r.Context(), `UPDATE cs_conversations SET unread_staff=0 WHERE id=$1`, id)
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type agentIn struct {
	Agent    string `json:"agent"`
	ClientID string `json:"clientId"`
	Text     string `json:"text"`
	ImageURL string `json:"imageUrl"`
}

func (c *CS) iAssign(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in agentIn
	_ = decodeLoose(r, &in)
	in.Agent = strings.TrimSpace(in.Agent)
	if in.Agent == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "agent 必填")
		return
	}
	res, err := c.srv.store.db.ExecContext(r.Context(),
		`UPDATE cs_conversations SET assignee=$2, status=CASE WHEN status IN ('resolved','closed') THEN status ELSE 'assigned' END, updated_at=now() WHERE id=$1`, id, in.Agent)
	if err != nil {
		intErr(w, "cs assign", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "after": map[string]any{"assignee": in.Agent, "status": "assigned"}})
}

func (c *CS) iReply(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in agentIn
	if err := decodeLoose(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.Agent = strings.TrimSpace(in.Agent)
	if in.Agent == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "agent 必填")
		return
	}
	cv, _, _, err := c.convByID(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	kind, text, imageURL := "text", "", ""
	if strings.TrimSpace(in.ImageURL) != "" {
		if !validImageURL(in.ImageURL) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "imageUrl 不合法")
			return
		}
		kind, imageURL = "image", in.ImageURL
	} else {
		text = cleanChatText(in.Text)
		if text == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "消息不能为空")
			return
		}
	}
	m, replayed, err := c.insert(r.Context(), cv.ID, in.Agent, "staff", strings.TrimSpace(in.ClientID), kind, text, imageURL)
	if err != nil {
		intErr(w, "cs reply", err)
		return
	}
	if replayed {
		common.WriteJSON(w, http.StatusOK, m)
		return
	}
	c.bump(r.Context(), cv.ID, false)
	// A reply implicitly assigns an unassigned conversation to the replying agent.
	_, _ = c.srv.store.db.ExecContext(r.Context(),
		`UPDATE cs_conversations SET assignee=CASE WHEN assignee='' THEN $2 ELSE assignee END,
		 status=CASE WHEN status IN ('resolved','closed') THEN status ELSE 'assigned' END WHERE id=$1`, cv.ID, in.Agent)
	c.broadcast(cv.PubToken, m)
	common.WriteJSON(w, http.StatusCreated, m)
}

func (c *CS) iStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in agentIn
		_ = decodeLoose(r, &in)
		var before string
		if err := c.srv.store.db.QueryRowContext(r.Context(), `SELECT status FROM cs_conversations WHERE id=$1`, id).Scan(&before); err != nil {
			if err == sql.ErrNoRows {
				common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
				return
			}
			intErr(w, "cs status", err)
			return
		}
		if _, err := c.srv.store.db.ExecContext(r.Context(), `UPDATE cs_conversations SET status=$2, updated_at=now() WHERE id=$1`, id, status); err != nil {
			intErr(w, "cs status", err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "before": map[string]any{"status": before}, "after": map[string]any{"status": status}})
	}
}

func (c *CS) adminRoutes(mux *http.ServeMux) {
	in := c.srv.internal
	mux.HandleFunc("GET /internal/cs/conversations", in(c.iList))
	mux.HandleFunc("GET /internal/cs/conversations/{id}/messages", in(c.iMessages))
	mux.HandleFunc("POST /internal/cs/conversations/{id}/assign", in(c.iAssign))
	mux.HandleFunc("POST /internal/cs/conversations/{id}/reply", in(c.iReply))
	mux.HandleFunc("POST /internal/cs/conversations/{id}/resolve", in(c.iStatus("resolved")))
	mux.HandleFunc("POST /internal/cs/conversations/{id}/close", in(c.iStatus("closed")))
}
