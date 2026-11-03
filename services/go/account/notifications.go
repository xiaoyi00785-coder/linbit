package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// notifications.go implements the message center: an in-app inbox per user
// (system / trade / activity / security tabs, per-item and "mark all" read)
// plus an email channel. Producers (this service, wallet, kyc, ...) post to
// POST /internal/notifications via common.Notify; the email copy is sent in the
// background through the configured EmailSender (Resend or the dev mock).

var noticeKinds = map[string]bool{
	common.NoticeSystem: true, common.NoticeTrade: true,
	common.NoticeActivity: true, common.NoticeSecurity: true,
}

// NotifyPrefs are the user's email channel switches. Security notices are
// always emailed and cannot be switched off.
type NotifyPrefs struct {
	EmailSystem   bool `json:"emailSystem"`
	EmailTrade    bool `json:"emailTrade"`
	EmailActivity bool `json:"emailActivity"`
	EmailSecurity bool `json:"emailSecurity"` // always true (read-only)
}

func defaultPrefs() NotifyPrefs {
	return NotifyPrefs{EmailSystem: true, EmailTrade: true, EmailActivity: false, EmailSecurity: true}
}

func (p NotifyPrefs) wants(kind string) bool {
	switch kind {
	case common.NoticeSecurity:
		return true
	case common.NoticeSystem:
		return p.EmailSystem
	case common.NoticeTrade:
		return p.EmailTrade
	case common.NoticeActivity:
		return p.EmailActivity
	}
	return false
}

type noticeDTO struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Time  int64  `json:"time"` // unix ms
	Read  bool   `json:"read"`
	Email string `json:"emailStatus"`
}

// ---- store ----

func (s *Store) NotifyPrefs(ctx context.Context, userID int64) (NotifyPrefs, error) {
	p := defaultPrefs()
	err := s.db.QueryRowContext(ctx,
		`SELECT email_system, email_trade, email_activity FROM notification_prefs WHERE user_id=$1`, userID).
		Scan(&p.EmailSystem, &p.EmailTrade, &p.EmailActivity)
	if err == sql.ErrNoRows {
		return p, nil
	}
	return p, err
}

func (s *Store) SetNotifyPrefs(ctx context.Context, userID int64, p NotifyPrefs) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_prefs (user_id, email_system, email_trade, email_activity, updated_at)
		VALUES ($1,$2,$3,$4,now())
		ON CONFLICT (user_id) DO UPDATE SET email_system=EXCLUDED.email_system,
		  email_trade=EXCLUDED.email_trade, email_activity=EXCLUDED.email_activity, updated_at=now()`,
		userID, p.EmailSystem, p.EmailTrade, p.EmailActivity)
	return err
}

func (s *Store) InsertNotification(ctx context.Context, userID int64, kind, title, body, emailStatus string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO notifications (user_id, kind, title, body, email_status)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, userID, kind, title, body, emailStatus).Scan(&id)
	return id, err
}

func (s *Store) SetNotificationEmailStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET email_status=$2 WHERE id=$1`, id, status)
	return err
}

// BroadcastNotification inserts one in-app notice for every active user
// (no email: broadcasts are announcements, not per-user events).
func (s *Store) BroadcastNotification(ctx context.Context, kind, title, body string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO notifications (user_id, kind, title, body)
		SELECT id, $1, $2, $3 FROM users WHERE status = 'active'`, kind, title, body)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) ListNotifications(ctx context.Context, userID int64, kind string, limit, offset int) ([]noticeDTO, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM notifications WHERE user_id=$1 AND ($2='' OR kind=$2)`, userID, kind).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, title, body, created_at, read_at IS NOT NULL, email_status
		FROM notifications WHERE user_id=$1 AND ($2='' OR kind=$2)
		ORDER BY id DESC LIMIT $3 OFFSET $4`, userID, kind, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []noticeDTO{}
	for rows.Next() {
		var d noticeDTO
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &d.Kind, &d.Title, &d.Body, &at, &d.Read, &d.Email); err != nil {
			return nil, 0, err
		}
		d.ID = strconv.FormatInt(id, 10)
		d.Time = at.UnixMilli()
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// UnreadCounts returns the unread count per kind.
func (s *Store) UnreadCounts(ctx context.Context, userID int64) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind, count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL GROUP BY kind`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for k := range noticeKinds {
		out[k] = 0
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// MarkNotificationsRead marks the given ids (scoped to the user) or, with
// all=true, every unread notice (optionally of one kind) as read.
func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64, ids []int64, all bool, kind string) (int64, error) {
	var res sql.Result
	var err error
	if all {
		res, err = s.db.ExecContext(ctx, `UPDATE notifications SET read_at=now()
			WHERE user_id=$1 AND read_at IS NULL AND ($2='' OR kind=$2)`, userID, kind)
	} else {
		res, err = s.db.ExecContext(ctx, `UPDATE notifications SET read_at=now()
			WHERE user_id=$1 AND read_at IS NULL AND id = ANY($2)`, userID, ids)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- delivery ----

// notify stores an in-app notice for u and, when the email channel applies
// (force, or the user's preference when force is nil), emails it in the
// background. Errors are logged; a notification never fails its caller.
func (s *Server) notify(ctx context.Context, u *User, kind, title, body string, force *bool) (int64, error) {
	wantEmail := false
	if force != nil {
		wantEmail = *force
	} else {
		p, err := s.store.NotifyPrefs(ctx, u.ID)
		if err != nil {
			return 0, err
		}
		wantEmail = p.wants(kind)
	}
	status := "none"
	to := ""
	if wantEmail {
		if u.Email.Valid && u.Email.String != "" {
			status, to = "pending", u.Email.String
		} else {
			status = "no_address"
		}
	}
	id, err := s.store.InsertNotification(ctx, u.ID, kind, title, body, status)
	if err != nil {
		return 0, err
	}
	if to != "" && s.mail != nil {
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			st := "sent"
			if err := s.mail.SendEmail(bg, to, "[Linbit] "+title, body); err != nil {
				log.Printf("notify email %d: %v", id, err)
				st = "failed"
			}
			_ = s.store.SetNotificationEmailStatus(bg, id, st)
		}()
	}
	return id, nil
}

// notifyAsync is a fire-and-forget wrapper used by in-service hooks.
func (s *Server) notifyAsync(u *User, kind, title, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notify(ctx, u, kind, title, body, nil); err != nil {
			log.Printf("notify %s: %v", kind, err)
		}
	}()
}

// ---- handlers ----

// GET /notifications?kind=&page=&pageSize=
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "" && !noticeKinds[kind] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的消息类型")
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	items, total, err := s.store.ListNotifications(r.Context(), u.ID, kind, size, (page-1)*size)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	unread, err := s.store.UnreadCounts(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "pageSize": size,
		"unread": sumCounts(unread), "unreadByKind": unread,
	})
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// GET /notifications/unread-count
func (s *Server) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	unread, err := s.store.UnreadCounts(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"unread": sumCounts(unread), "unreadByKind": unread})
}

// POST /notifications/read {ids:["1","2"]} | {all:true, kind?}
func (s *Server) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	var req struct {
		IDs  []string `json:"ids"`
		All  bool     `json:"all"`
		Kind string   `json:"kind"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if req.Kind != "" && !noticeKinds[req.Kind] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的消息类型")
		return
	}
	if !req.All && (len(req.IDs) == 0 || len(req.IDs) > 200) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请指定消息或全部已读")
		return
	}
	ids := make([]int64, 0, len(req.IDs))
	for _, v := range req.IDs {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的消息编号")
			return
		}
		ids = append(ids, id)
	}
	n, err := s.store.MarkNotificationsRead(r.Context(), u.ID, ids, req.All, req.Kind)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"updated": n})
}

// GET /notifications/settings
func (s *Server) handleGetNotifyPrefs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	p, err := s.store.NotifyPrefs(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	p.EmailSecurity = true
	common.WriteJSON(w, http.StatusOK, p)
}

// PUT /notifications/settings {emailSystem, emailTrade, emailActivity}
func (s *Server) handleSetNotifyPrefs(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	var req struct {
		EmailSystem   *bool `json:"emailSystem"`
		EmailTrade    *bool `json:"emailTrade"`
		EmailActivity *bool `json:"emailActivity"`
		EmailSecurity *bool `json:"emailSecurity"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if req.EmailSecurity != nil && !*req.EmailSecurity {
		common.WriteError(w, http.StatusBadRequest, "SECURITY_EMAIL_REQUIRED", "安全通知邮件不可关闭")
		return
	}
	p, err := s.store.NotifyPrefs(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if req.EmailSystem != nil {
		p.EmailSystem = *req.EmailSystem
	}
	if req.EmailTrade != nil {
		p.EmailTrade = *req.EmailTrade
	}
	if req.EmailActivity != nil {
		p.EmailActivity = *req.EmailActivity
	}
	if err := s.store.SetNotifyPrefs(r.Context(), u.ID, p); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	p.EmailSecurity = true
	common.WriteJSON(w, http.StatusOK, p)
}

func validNotice(kind, title, body string) bool {
	return noticeKinds[kind] && strings.TrimSpace(title) != "" && len(title) <= 200 &&
		strings.TrimSpace(body) != "" && len(body) <= 4000
}

// POST /internal/notifications {uid, kind, title, body, email?}
func (s *Server) handleInternalNotify(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
		return
	}
	var n common.Notice
	if err := common.DecodeJSON(r, &n); err != nil || !validNotice(n.Kind, n.Title, n.Body) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "通知参数无效")
		return
	}
	u, err := s.store.UserByUID(r.Context(), n.UID)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "用户不存在")
		return
	}
	id, err := s.notify(r.Context(), u, n.Kind, strings.TrimSpace(n.Title), strings.TrimSpace(n.Body), n.Email)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": strconv.FormatInt(id, 10)})
}

// POST /internal/notifications/broadcast {kind, title, body} (admin announcements)
func (s *Server) handleInternalBroadcast(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
		return
	}
	var req struct{ Kind, Title, Body string }
	if err := common.DecodeJSON(r, &req); err != nil || !validNotice(req.Kind, req.Title, req.Body) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "通知参数无效")
		return
	}
	n, err := s.store.BroadcastNotification(r.Context(), req.Kind, strings.TrimSpace(req.Title), strings.TrimSpace(req.Body))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"recipients": n})
}
