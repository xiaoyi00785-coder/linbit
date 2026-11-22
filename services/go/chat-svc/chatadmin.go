package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// chatadmin.go: internal moderation endpoints, called only by the admin
// service (which enforces RBAC, 2FA, reason and audit). Every write returns
// {before, after} for the audit log.

func (c *Chat) adminRoutes(mux *http.ServeMux) {
	in := c.srv.internal
	mux.HandleFunc("GET /internal/chat/config", in(c.iConfigGet))
	mux.HandleFunc("PUT /internal/chat/config", in(c.iConfigPut))
	mux.HandleFunc("GET /internal/chat/messages", in(c.iMessages))
	mux.HandleFunc("DELETE /internal/chat/messages/{id}", in(c.iDelete))
	mux.HandleFunc("GET /internal/chat/sanctions", in(c.iSanctions))
	mux.HandleFunc("POST /internal/chat/mutes", in(c.iSanction("mute")))
	mux.HandleFunc("DELETE /internal/chat/mutes/{uid}", in(c.iSanction("unmute")))
	mux.HandleFunc("POST /internal/chat/bans", in(c.iSanction("ban")))
	mux.HandleFunc("DELETE /internal/chat/bans/{uid}", in(c.iSanction("unban")))
	mux.HandleFunc("GET /internal/chat/reports", in(c.iReports))
	mux.HandleFunc("POST /internal/chat/reports/{id}/resolve", in(c.iResolve))
}

func decodeLoose(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(v)
}

func (c *Chat) iConfigGet(w http.ResponseWriter, r *http.Request) {
	c.invalidate()
	common.WriteJSON(w, http.StatusOK, map[string]any{"config": c.config(r.Context()), "defaults": defaultChatConfig()})
}

// PUT: partial update — only the keys sent change.
func (c *Chat) iConfigPut(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	by := ""
	if v, ok := raw["updatedBy"]; ok {
		_ = json.Unmarshal(v, &by)
		delete(raw, "updatedBy")
	}
	delete(raw, "reason")
	c.invalidate()
	before := c.config(r.Context())
	cur, _ := json.Marshal(before)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(cur, &merged)
	for k, v := range raw {
		if _, ok := merged[k]; !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "未知配置项: "+k)
			return
		}
		merged[k] = v
	}
	b, _ := json.Marshal(merged)
	var after ChatConfig
	if err := json.Unmarshal(b, &after); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "配置类型错误")
		return
	}
	if after.BannedWords == nil {
		after.BannedWords = []string{}
	}
	if err := after.validate(); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	nb, _ := json.Marshal(after)
	if _, err := c.srv.store.db.ExecContext(r.Context(), `INSERT INTO chat_config (id, cfg, updated_by, updated_at) VALUES (1,$1,$2,now())
		ON CONFLICT (id) DO UPDATE SET cfg=$1, updated_by=$2, updated_at=now()`, nb, by); err != nil {
		intErr(w, "chat config put", err)
		return
	}
	c.invalidate()
	common.WriteJSON(w, http.StatusOK, map[string]any{"config": after, "before": before, "after": after})
}

type adminChatMessage struct {
	ID            int64     `json:"id"`
	Room          string    `json:"room"`
	UID           string    `json:"uid"`
	Text          string    `json:"text"`
	Status        string    `json:"status"`
	DeletedBy     string    `json:"deletedBy"`
	DeletedReason string    `json:"deletedReason"`
	Reports       int       `json:"reports"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (c *Chat) iMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := []string{"true"}, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if v := q.Get("room"); v != "" {
		add("m.room = ?", v)
	}
	if v := q.Get("uid"); v != "" {
		add("m.uid = ?", v)
	}
	if v := q.Get("status"); v != "" {
		add("m.status = ?", v)
	}
	if v := q.Get("q"); v != "" {
		add("m.text ILIKE '%' || ? || '%'", v)
	}
	if v := atoiDefault(q.Get("before"), 0, 0, 1<<62); v > 0 {
		add("m.id < ?", v)
	}
	limit := atoiDefault(q.Get("limit"), 50, 1, 200)
	rows, err := c.srv.store.db.QueryContext(r.Context(), `SELECT m.id, m.room, m.uid, m.text, m.status, m.deleted_by, m.deleted_reason,
		(SELECT count(*) FROM chat_reports x WHERE x.message_id=m.id), m.created_at
		FROM chat_messages m WHERE `+strings.Join(where, " AND ")+` ORDER BY m.id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		intErr(w, "chat admin messages", err)
		return
	}
	defer rows.Close()
	items := []adminChatMessage{}
	for rows.Next() {
		var m adminChatMessage
		if err := rows.Scan(&m.ID, &m.Room, &m.UID, &m.Text, &m.Status, &m.DeletedBy, &m.DeletedReason, &m.Reports, &m.CreatedAt); err != nil {
			intErr(w, "chat admin messages", err)
			return
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type modIn struct {
	UID             string `json:"uid"`
	DurationMinutes int    `json:"durationMinutes"`
	Reason          string `json:"reason"`
	UpdatedBy       string `json:"updatedBy"`
	Action          string `json:"action"`
}

func (c *Chat) iDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	var in modIn
	_ = decodeLoose(r, &in)
	m, err := c.deleteMessage(r.Context(), id, in.UpdatedBy, in.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "消息不存在或已删除")
		return
	}
	if err != nil {
		intErr(w, "chat delete", err)
		return
	}
	// Reports on a deleted message are resolved.
	_, _ = c.srv.store.db.ExecContext(r.Context(), `UPDATE chat_reports SET status='resolved', handled_by=$2, handled_at=now() WHERE message_id=$1 AND status='open'`, id, in.UpdatedBy)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "before": map[string]any{"id": m.ID, "room": m.Room, "uid": m.Sender, "text": m.Text, "status": "visible"},
		"after": map[string]any{"id": m.ID, "status": "deleted"}})
}

type sanction struct {
	UID        string     `json:"uid"`
	MutedUntil *time.Time `json:"mutedUntil"`
	Banned     bool       `json:"banned"`
	Reason     string     `json:"reason"`
	UpdatedBy  string     `json:"updatedBy"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func (c *Chat) getSanction(r *http.Request, uid string) *sanction {
	var s sanction
	var mu sql.NullTime
	if err := c.srv.store.db.QueryRowContext(r.Context(), `SELECT uid, muted_until, banned, reason, updated_by, updated_at FROM chat_sanctions WHERE uid=$1`, uid).
		Scan(&s.UID, &mu, &s.Banned, &s.Reason, &s.UpdatedBy, &s.UpdatedAt); err != nil {
		return nil
	}
	if mu.Valid {
		t := mu.Time.UTC()
		s.MutedUntil = &t
	}
	return &s
}

func (c *Chat) iSanctions(w http.ResponseWriter, r *http.Request) {
	rows, err := c.srv.store.db.QueryContext(r.Context(), `SELECT uid, muted_until, banned, reason, updated_by, updated_at FROM chat_sanctions
		WHERE banned OR muted_until > now() ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		intErr(w, "chat sanctions", err)
		return
	}
	defer rows.Close()
	items := []sanction{}
	for rows.Next() {
		var s sanction
		var mu sql.NullTime
		if err := rows.Scan(&s.UID, &mu, &s.Banned, &s.Reason, &s.UpdatedBy, &s.UpdatedAt); err != nil {
			intErr(w, "chat sanctions", err)
			return
		}
		if mu.Valid {
			t := mu.Time.UTC()
			s.MutedUntil = &t
		}
		items = append(items, s)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (c *Chat) iSanction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in modIn
		if err := decodeLoose(r, &in); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
		if v := r.PathValue("uid"); v != "" {
			in.UID = v
		}
		in.UID = strings.TrimSpace(in.UID)
		if in.UID == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "uid 必填")
			return
		}
		before := c.getSanction(r, in.UID)
		var q string
		args := []any{in.UID, in.Reason, in.UpdatedBy}
		switch action {
		case "mute":
			if in.DurationMinutes < 1 || in.DurationMinutes > 525600 {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "durationMinutes 须在 1-525600")
				return
			}
			args = append(args, in.DurationMinutes)
			q = `INSERT INTO chat_sanctions (uid, reason, updated_by, muted_until) VALUES ($1,$2,$3, now() + make_interval(mins => $4))
				ON CONFLICT (uid) DO UPDATE SET muted_until=now() + make_interval(mins => $4), reason=$2, updated_by=$3, updated_at=now()`
		case "unmute":
			q = `UPDATE chat_sanctions SET muted_until=NULL, reason=$2, updated_by=$3, updated_at=now() WHERE uid=$1`
		case "ban":
			q = `INSERT INTO chat_sanctions (uid, reason, updated_by, banned) VALUES ($1,$2,$3,true)
				ON CONFLICT (uid) DO UPDATE SET banned=true, reason=$2, updated_by=$3, updated_at=now()`
		case "unban":
			q = `UPDATE chat_sanctions SET banned=false, reason=$2, updated_by=$3, updated_at=now() WHERE uid=$1`
		}
		if _, err := c.srv.store.db.ExecContext(r.Context(), q, args...); err != nil {
			intErr(w, "chat "+action, err)
			return
		}
		after := c.getSanction(r, in.UID)
		common.WriteJSON(w, http.StatusOK, map[string]any{"sanction": after, "before": before, "after": after})
	}
}

func (c *Chat) iReports(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 50, 1, 200)
	rows, err := c.srv.store.db.QueryContext(r.Context(), `SELECT p.id, p.message_id, p.reporter, p.reason, p.status, p.handled_by, p.created_at,
		m.room, m.uid, m.text, m.status
		FROM chat_reports p JOIN chat_messages m ON m.id=p.message_id
		WHERE ($1='all' OR p.status=$1) ORDER BY p.id DESC LIMIT `+strconv.Itoa(limit), status)
	if err != nil {
		intErr(w, "chat reports", err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, mid int64
		var reporter, reason, st, by, room, uid, text, mst string
		var at time.Time
		if err := rows.Scan(&id, &mid, &reporter, &reason, &st, &by, &at, &room, &uid, &text, &mst); err != nil {
			intErr(w, "chat reports", err)
			return
		}
		items = append(items, map[string]any{"id": id, "messageId": mid, "reporter": reporter, "reason": reason, "status": st,
			"handledBy": by, "createdAt": at, "message": map[string]any{"room": room, "uid": uid, "text": text, "status": mst}})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /internal/chat/reports/{id}/resolve {action: resolve|dismiss, updatedBy}
func (c *Chat) iResolve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	var in modIn
	_ = decodeLoose(r, &in)
	st := map[string]string{"resolve": "resolved", "dismiss": "dismissed"}[in.Action]
	if st == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "action 须为 resolve 或 dismiss")
		return
	}
	var prev string
	if err := c.srv.store.db.QueryRowContext(r.Context(), `SELECT status FROM chat_reports WHERE id=$1`, id).Scan(&prev); err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "举报不存在")
		return
	}
	if _, err := c.srv.store.db.ExecContext(r.Context(), `UPDATE chat_reports SET status=$2, handled_by=$3, handled_at=now() WHERE id=$1`, id, st, in.UpdatedBy); err != nil {
		intErr(w, "chat resolve", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "before": map[string]any{"status": prev}, "after": map[string]any{"status": st}})
}
