package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/common/storage"
)

// support.go: 站内客服工单 (support tickets).
//
// User (gateway, JWT):
//	POST /support/attachments               multipart "file" (jpeg/png/webp, <= TICKET_ATTACH_MAX_BYTES)
//	GET  /support/attachments/{id}          owner only
//	GET  /support/tickets?status=&page=&pageSize=
//	POST /support/tickets                   {category, subject, content, attachments[]}
//	GET  /support/tickets/{id}              thread (marks support replies read)
//	POST /support/tickets/{id}/messages     {content, attachments[]}
//	POST /support/tickets/{id}/close
// Internal (admin service, X-Internal-Token):
//	GET   /internal/admin/support/tickets?status=&category=&uid=&assigneeId=&page=&pageSize=
//	GET   /internal/admin/support/tickets/{id}
//	POST  /internal/admin/support/tickets/{id}/messages  {content, adminId, adminName, status?}
//	PATCH /internal/admin/support/tickets/{id}           {status?, assigneeId?, assigneeName?, adminName}
//	GET   /internal/admin/support/attachments/{id}
//
// User text is stored sanitised: control/bidi characters removed and links
// defanged (https[:]//, www[.]) so no client renders them clickable. Clients
// must render bodies as plain text. Rate limits are DB counters per user.

var ticketCategories = []string{"account", "deposit_withdraw", "trade", "kyc", "security", "other"}

func validCategory(c string) bool {
	for _, k := range ticketCategories {
		if k == c {
			return true
		}
	}
	return false
}

const (
	ticketSubjectMax  = 120
	ticketBodyMax     = 5000
	ticketAttachMax   = 3
	ticketStatusOpen  = "open"
	ticketStatusReply = "replied"
	ticketStatusClose = "closed"
)

var (
	errTicketNotFound = errors.New("ticket not found")
	errTicketClosed   = errors.New("ticket closed")
	errAttachInvalid  = errors.New("attachment invalid")
	errRateLimited    = errors.New("rate limited")
)

// ---- sanitising ----

var (
	reScheme  = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]{1,15}):/+`)
	reDanger  = regexp.MustCompile(`(?i)\b(javascript|vbscript|data|file|mailto|tel|sms):`)
	reWWW     = regexp.MustCompile(`(?i)\bwww\.`)
	reBlankLn = regexp.MustCompile(`\n{4,}`)
)

// cleanText drops control and bidi-override characters, normalises newlines
// and trims; it does not touch links (see defangLinks).
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F, r == 0xFEFF:
			return -1
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(reBlankLn.ReplaceAllString(s, "\n\n\n"))
}

// defangLinks makes URLs inert: "https://x.io" -> "https[:]//x.io",
// "javascript:" -> "javascript[:]", "www.x.io" -> "www[.]x.io".
func defangLinks(s string) string {
	s = reScheme.ReplaceAllString(s, "$1[:]//")
	s = reDanger.ReplaceAllString(s, "$1[:]")
	return reWWW.ReplaceAllString(s, "www[.]")
}

func sanitizeUserText(s string) string { return defangLinks(cleanText(s)) }

// ---- DTOs ----

type ticketAttachment struct {
	ID   string `json:"id"`
	MIME string `json:"mime"`
	Size int    `json:"size"`
	URL  string `json:"url,omitempty"`
}

type ticketMessage struct {
	ID          string             `json:"id"`
	Author      string             `json:"author"` // user | support
	AuthorName  string             `json:"authorName,omitempty"`
	Body        string             `json:"body"`
	Attachments []ticketAttachment `json:"attachments"`
	CreatedAt   time.Time          `json:"createdAt"`
}

type ticketDTO struct {
	ID           string          `json:"id"`
	UID          string          `json:"uid,omitempty"`
	Category     string          `json:"category"`
	Subject      string          `json:"subject"`
	Status       string          `json:"status"`
	AssigneeID   *int64          `json:"assigneeId,omitempty"`
	AssigneeName string          `json:"assigneeName,omitempty"`
	LastReplyBy  string          `json:"lastReplyBy"`
	LastReplyAt  time.Time       `json:"lastReplyAt"`
	Unread       bool            `json:"unread"`
	MessageCount int             `json:"messageCount"`
	ClosedAt     *time.Time      `json:"closedAt,omitempty"`
	ClosedBy     string          `json:"closedBy,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Messages     []ticketMessage `json:"messages,omitempty"`
	userID       int64
}

const ticketCols = `t.id, u.uid, t.user_id, t.category, t.subject, t.status, t.assignee_id, COALESCE(t.assignee_name,''),
	t.last_reply_by, t.last_reply_at, (t.last_support_at IS NOT NULL AND t.last_support_at > t.user_read_at),
	(SELECT count(*) FROM support_ticket_messages m WHERE m.ticket_id=t.id), t.closed_at, COALESCE(t.closed_by,''), t.created_at, t.updated_at`

func scanTicket(sc interface{ Scan(...any) error }) (*ticketDTO, error) {
	var t ticketDTO
	var id int64
	var asg sql.NullInt64
	var closed sql.NullTime
	if err := sc.Scan(&id, &t.UID, &t.userID, &t.Category, &t.Subject, &t.Status, &asg, &t.AssigneeName, &t.LastReplyBy,
		&t.LastReplyAt, &t.Unread, &t.MessageCount, &closed, &t.ClosedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.ID = strconv.FormatInt(id, 10)
	if asg.Valid {
		v := asg.Int64
		t.AssigneeID = &v
	}
	if closed.Valid {
		c := closed.Time
		t.ClosedAt = &c
	}
	return &t, nil
}

// ---- store ----

type ticketFilter struct {
	UserID     int64
	Status     string
	Category   string
	UID        string
	AssigneeID int64 // -1 = unassigned
}

func (s *Store) ListTickets(ctx context.Context, f ticketFilter, limit, offset int) ([]*ticketDTO, int, error) {
	where := []string{"TRUE"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if f.UserID != 0 {
		add("t.user_id=?", f.UserID)
	}
	if f.Status != "" {
		add("t.status=?", f.Status)
	}
	if f.Category != "" {
		add("t.category=?", f.Category)
	}
	if f.UID != "" {
		add("u.uid=?", f.UID)
	}
	switch {
	case f.AssigneeID > 0:
		add("t.assignee_id=?", f.AssigneeID)
	case f.AssigneeID < 0:
		where = append(where, "t.assignee_id IS NULL")
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM support_tickets t JOIN users u ON u.id=t.user_id WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "t.id DESC"
	if f.UserID == 0 { // back office: oldest waiting first among open, else newest activity
		order = "(t.status='open') DESC, CASE WHEN t.status='open' THEN t.last_reply_at END ASC, t.last_reply_at DESC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+ticketCols+` FROM support_tickets t JOIN users u ON u.id=t.user_id WHERE `+w+
		` ORDER BY `+order+` LIMIT `+strconv.Itoa(limit)+` OFFSET `+strconv.Itoa(offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*ticketDTO{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func (s *Store) GetTicket(ctx context.Context, id int64) (*ticketDTO, error) {
	t, err := scanTicket(s.db.QueryRowContext(ctx, `SELECT `+ticketCols+` FROM support_tickets t JOIN users u ON u.id=t.user_id WHERE t.id=$1`, id))
	if err == sql.ErrNoRows {
		return nil, errTicketNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.author_type, m.author_name, m.body, m.created_at,
		COALESCE(json_agg(json_build_array(a.id, a.mime, a.size_bytes) ORDER BY a.created_at) FILTER (WHERE a.id IS NOT NULL), '[]')
		FROM support_ticket_messages m LEFT JOIN support_attachments a ON a.message_id=m.id
		WHERE m.ticket_id=$1 GROUP BY m.id ORDER BY m.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t.Messages = []ticketMessage{}
	for rows.Next() {
		var m ticketMessage
		var mid int64
		var att []byte
		if err := rows.Scan(&mid, &m.Author, &m.AuthorName, &m.Body, &m.CreatedAt, &att); err != nil {
			return nil, err
		}
		m.ID = strconv.FormatInt(mid, 10)
		m.Attachments = parseAttachRows(att)
		t.Messages = append(t.Messages, m)
	}
	return t, rows.Err()
}

func parseAttachRows(b []byte) []ticketAttachment {
	var raw [][3]any
	out := []ticketAttachment{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return out
	}
	for _, r := range raw {
		id, _ := r[0].(string)
		mime, _ := r[1].(string)
		size, _ := r[2].(float64)
		out = append(out, ticketAttachment{ID: id, MIME: mime, Size: int(size)})
	}
	return out
}

// lockUser serialises a user's ticket writes so the rate-limit counts and the
// writes they guard are consistent.
func lockUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7130, $1::int)`, userID%2147483647)
	return err
}

func (s *Server) ticketLimits() (perDay, openMax, msgsHour, uploadsHour int) {
	d := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return v
	}
	return d(s.cfg.TicketsPerDay, 5), d(s.cfg.TicketsOpenMax, 5), d(s.cfg.TicketMessagesPerHour, 30), d(s.cfg.TicketUploadsPerHour, 20)
}

// bindAttachments attaches the user's unbound uploads to a message.
func bindAttachments(ctx context.Context, tx *sql.Tx, userID, msgID int64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > ticketAttachMax {
		return errAttachInvalid
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return errAttachInvalid
		}
		seen[id] = true
		res, err := tx.ExecContext(ctx, `UPDATE support_attachments SET message_id=$3 WHERE id=$1 AND user_id=$2 AND message_id IS NULL`, id, userID, msgID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errAttachInvalid
		}
	}
	return nil
}

func (s *Server) createTicket(ctx context.Context, u *User, category, subject, body string, att []string) (int64, error) {
	perDay, openMax, msgsHour, _ := s.ticketLimits()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockUser(ctx, tx, u.ID); err != nil {
		return 0, err
	}
	var day, open, msgs int
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM support_tickets WHERE user_id=$1 AND created_at > now()-interval '24 hours'),
		(SELECT count(*) FROM support_tickets WHERE user_id=$1 AND status<>'closed'),
		(SELECT count(*) FROM support_ticket_messages WHERE author_type='user' AND author_id=$1 AND created_at > now()-interval '1 hour')`,
		u.ID).Scan(&day, &open, &msgs); err != nil {
		return 0, err
	}
	if day >= perDay || open >= openMax || msgs >= msgsHour {
		return 0, errRateLimited
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO support_tickets (user_id, category, subject) VALUES ($1,$2,$3) RETURNING id`,
		u.ID, category, subject).Scan(&id); err != nil {
		return 0, err
	}
	var mid int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO support_ticket_messages (ticket_id, author_type, author_id, body) VALUES ($1,'user',$2,$3) RETURNING id`,
		id, u.ID, body).Scan(&mid); err != nil {
		return 0, err
	}
	if err := bindAttachments(ctx, tx, u.ID, mid, att); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Server) userReply(ctx context.Context, u *User, ticketID int64, body string, att []string) error {
	_, _, msgsHour, _ := s.ticketLimits()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockUser(ctx, tx, u.ID); err != nil {
		return err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM support_tickets WHERE id=$1 AND user_id=$2 FOR UPDATE`, ticketID, u.ID).Scan(&status)
	if err == sql.ErrNoRows {
		return errTicketNotFound
	}
	if err != nil {
		return err
	}
	if status == ticketStatusClose {
		return errTicketClosed
	}
	var msgs int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM support_ticket_messages WHERE author_type='user' AND author_id=$1 AND created_at > now()-interval '1 hour'`, u.ID).Scan(&msgs); err != nil {
		return err
	}
	if msgs >= msgsHour {
		return errRateLimited
	}
	var mid int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO support_ticket_messages (ticket_id, author_type, author_id, body) VALUES ($1,'user',$2,$3) RETURNING id`,
		ticketID, u.ID, body).Scan(&mid); err != nil {
		return err
	}
	if err := bindAttachments(ctx, tx, u.ID, mid, att); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE support_tickets SET status='open', last_reply_by='user', last_reply_at=now(), user_read_at=now(), updated_at=now() WHERE id=$1`, ticketID); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- helpers ----

func ticketErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTicketNotFound):
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "工单不存在")
	case errors.Is(err, errTicketClosed):
		common.WriteError(w, http.StatusConflict, "TICKET_CLOSED", "工单已关闭，如有新问题请重新提交")
	case errors.Is(err, errAttachInvalid):
		common.WriteError(w, http.StatusBadRequest, "ATTACHMENT_INVALID", "附件无效、已被使用或超过 3 张")
	case errors.Is(err, errRateLimited):
		w.Header().Set("Retry-After", "3600")
		common.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "提交过于频繁，请稍后再试")
	default:
		log.Printf("support: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
}

func pathTicketID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func pageOf(r *http.Request) (page, size int) {
	q := r.URL.Query()
	page, _ = strconv.Atoi(q.Get("page"))
	size, _ = strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return
}

type ticketInput struct {
	Category    string   `json:"category"`
	Subject     string   `json:"subject"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments"`
}

func validText(s string, max int) bool { return s != "" && utf8.RuneCountInString(s) <= max }

func userView(t *ticketDTO) *ticketDTO {
	t.UID, t.AssigneeID, t.AssigneeName = "", nil, ""
	for i := range t.Messages {
		if t.Messages[i].Author == "support" {
			t.Messages[i].AuthorName = "客服"
		} else {
			t.Messages[i].AuthorName = ""
		}
		for j := range t.Messages[i].Attachments {
			t.Messages[i].Attachments[j].URL = "/support/attachments/" + t.Messages[i].Attachments[j].ID
		}
	}
	return t
}

func (s *Server) userTicket(w http.ResponseWriter, r *http.Request) (*User, *ticketDTO, bool) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return nil, nil, false
	}
	id, ok := pathTicketID(r)
	if !ok {
		ticketErr(w, errTicketNotFound)
		return nil, nil, false
	}
	t, err := s.store.GetTicket(r.Context(), id)
	if err == nil && t.userID != u.ID {
		err = errTicketNotFound // never reveal other users' tickets
	}
	if err != nil {
		ticketErr(w, err)
		return nil, nil, false
	}
	return u, t, true
}

// ---- user handlers ----

// POST /support/attachments (multipart, field "file")
func (s *Server) handleTicketUpload(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	if s.tickets == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "附件存储不可用")
		return
	}
	_, _, _, perHour := s.ticketLimits()
	var n int
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM support_attachments WHERE user_id=$1 AND created_at > now()-interval '1 hour'`, u.ID).Scan(&n); err != nil {
		ticketErr(w, err)
		return
	}
	if n >= perHour {
		ticketErr(w, errRateLimited)
		return
	}
	maxBytes := s.cfg.TicketAttachMaxBytes
	if maxBytes <= 0 {
		maxBytes = 5 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+4096)
	if err := r.ParseMultipartForm(maxBytes + 4096); err != nil {
		common.WriteError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件过大或表单无效")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少上传文件字段 file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(data) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "文件为空或读取失败")
		return
	}
	if int64(len(data)) > maxBytes {
		common.WriteError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "图片超过大小限制")
		return
	}
	it, err := storage.DetectImageType(data)
	if err != nil {
		common.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "仅支持 JPEG/PNG/WEBP 图片")
		return
	}
	id := randomName() + "." + it.Ext
	key := u.UID + "/" + id
	if _, err := s.tickets.Put(r.Context(), key, it.MIME, data); err != nil {
		ticketErr(w, err)
		return
	}
	if _, err := s.store.db.ExecContext(r.Context(), `INSERT INTO support_attachments (id, user_id, storage_key, mime, size_bytes) VALUES ($1,$2,$3,$4,$5)`,
		id, u.ID, key, it.MIME, len(data)); err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, ticketAttachment{ID: id, MIME: it.MIME, Size: len(data), URL: "/support/attachments/" + id})
}

func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, ownerID int64) {
	var key, mime string
	var uid int64
	err := s.store.db.QueryRowContext(r.Context(), `SELECT storage_key, mime, user_id FROM support_attachments WHERE id=$1`, r.PathValue("id")).Scan(&key, &mime, &uid)
	if err != nil || (ownerID != 0 && uid != ownerID) || s.tickets == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "附件不存在")
		return
	}
	rd, ok := s.tickets.(interface {
		Open(context.Context, string) (io.ReadCloser, string, error)
	})
	if !ok {
		common.WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "当前存储后端暂不支持读取")
		return
	}
	f, _, err := rd.Open(r.Context(), key)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "附件不存在")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, f)
}

// GET /support/attachments/{id}
func (s *Server) handleTicketAttachment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	s.serveAttachment(w, r, u.ID)
}

// GET /support/tickets
func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	st := r.URL.Query().Get("status")
	if st != "" && st != ticketStatusOpen && st != ticketStatusReply && st != ticketStatusClose {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的状态")
		return
	}
	page, size := pageOf(r)
	items, total, err := s.store.ListTickets(r.Context(), ticketFilter{UserID: u.ID, Status: st}, size, (page-1)*size)
	if err != nil {
		ticketErr(w, err)
		return
	}
	for _, t := range items {
		userView(t)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size, "categories": ticketCategories})
}

// POST /support/tickets
func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	var in ticketInput
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	cat := strings.TrimSpace(in.Category)
	subject := strings.ReplaceAll(sanitizeUserText(in.Subject), "\n", " ")
	body := sanitizeUserText(in.Content)
	switch {
	case !validCategory(cat):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请选择问题分类")
		return
	case !validText(subject, ticketSubjectMax):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "标题必填，最多 120 字")
		return
	case !validText(body, ticketBodyMax):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "问题描述必填，最多 5000 字")
		return
	}
	id, err := s.createTicket(r.Context(), u, cat, subject, body, in.Attachments)
	if err != nil {
		ticketErr(w, err)
		return
	}
	t, err := s.store.GetTicket(r.Context(), id)
	if err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, userView(t))
}

// GET /support/tickets/{id}
func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	_, t, ok := s.userTicket(w, r)
	if !ok {
		return
	}
	if t.Unread {
		_, _ = s.store.db.ExecContext(r.Context(), `UPDATE support_tickets SET user_read_at=now() WHERE id=$1`, mustID(t.ID))
		t.Unread = false
	}
	common.WriteJSON(w, http.StatusOK, userView(t))
}

// POST /support/tickets/{id}/messages
func (s *Server) handleTicketReply(w http.ResponseWriter, r *http.Request) {
	u, t, ok := s.userTicket(w, r)
	if !ok {
		return
	}
	var in ticketInput
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	body := sanitizeUserText(in.Content)
	if !validText(body, ticketBodyMax) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "回复内容必填，最多 5000 字")
		return
	}
	id, _ := strconv.ParseInt(t.ID, 10, 64)
	if err := s.userReply(r.Context(), u, id, body, in.Attachments); err != nil {
		ticketErr(w, err)
		return
	}
	t, err := s.store.GetTicket(r.Context(), id)
	if err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, userView(t))
}

// POST /support/tickets/{id}/close
func (s *Server) handleCloseTicket(w http.ResponseWriter, r *http.Request) {
	_, t, ok := s.userTicket(w, r)
	if !ok {
		return
	}
	if t.Status != ticketStatusClose {
		if _, err := s.store.db.ExecContext(r.Context(), `UPDATE support_tickets SET status='closed', closed_at=now(), closed_by='user', updated_at=now() WHERE id=$1 AND status<>'closed'`, mustID(t.ID)); err != nil {
			ticketErr(w, err)
			return
		}
	}
	id, _ := strconv.ParseInt(t.ID, 10, 64)
	t, err := s.store.GetTicket(r.Context(), id)
	if err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, userView(t))
}

// ---- internal (admin) handlers ----

func (s *Server) internalOnly(w http.ResponseWriter, r *http.Request) bool {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
		return false
	}
	return true
}

// GET /internal/admin/support/tickets
func (s *Server) handleInternalListTickets(w http.ResponseWriter, r *http.Request) {
	if !s.internalOnly(w, r) {
		return
	}
	q := r.URL.Query()
	f := ticketFilter{Status: q.Get("status"), Category: q.Get("category"), UID: strings.TrimSpace(q.Get("uid"))}
	if f.Status == "all" {
		f.Status = ""
	}
	if f.Status != "" && f.Status != ticketStatusOpen && f.Status != ticketStatusReply && f.Status != ticketStatusClose {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的状态")
		return
	}
	if f.Category != "" && !validCategory(f.Category) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的分类")
		return
	}
	switch a := q.Get("assigneeId"); a {
	case "":
	case "none":
		f.AssigneeID = -1
	default:
		n, err := strconv.ParseInt(a, 10, 64)
		if err != nil || n <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的处理人")
			return
		}
		f.AssigneeID = n
	}
	page, size := pageOf(r)
	items, total, err := s.store.ListTickets(r.Context(), f, size, (page-1)*size)
	if err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size, "categories": ticketCategories})
}

func (s *Server) internalTicket(w http.ResponseWriter, r *http.Request) (int64, *ticketDTO, bool) {
	id, ok := pathTicketID(r)
	if !ok {
		ticketErr(w, errTicketNotFound)
		return 0, nil, false
	}
	t, err := s.store.GetTicket(r.Context(), id)
	if err != nil {
		ticketErr(w, err)
		return 0, nil, false
	}
	return id, t, true
}

// GET /internal/admin/support/tickets/{id}
func (s *Server) handleInternalGetTicket(w http.ResponseWriter, r *http.Request) {
	if !s.internalOnly(w, r) {
		return
	}
	if _, t, ok := s.internalTicket(w, r); ok {
		common.WriteJSON(w, http.StatusOK, t)
	}
}

// POST /internal/admin/support/tickets/{id}/messages {content, adminId, adminName, status?}
func (s *Server) handleInternalTicketReply(w http.ResponseWriter, r *http.Request) {
	if !s.internalOnly(w, r) {
		return
	}
	var in struct {
		Content   string `json:"content"`
		AdminID   int64  `json:"adminId"`
		AdminName string `json:"adminName"`
		Status    string `json:"status"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	body := cleanText(in.Content)
	status := in.Status
	if status == "" {
		status = ticketStatusReply
	}
	if !validText(body, ticketBodyMax) || in.AdminID <= 0 || (status != ticketStatusReply && status != ticketStatusClose) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "回复内容必填（最多 5000 字），状态只能为 replied/closed")
		return
	}
	id, t, ok := s.internalTicket(w, r)
	if !ok {
		return
	}
	before := t.Status
	tx, err := s.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		ticketErr(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO support_ticket_messages (ticket_id, author_type, author_id, author_name, body) VALUES ($1,'support',$2,$3,$4)`,
		id, in.AdminID, in.AdminName, body); err != nil {
		ticketErr(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE support_tickets SET status=$2, last_reply_by='support', last_reply_at=now(), last_support_at=now(), updated_at=now(),
		assignee_id=COALESCE(assignee_id,$3), assignee_name=COALESCE(assignee_name,$4),
		closed_at=CASE WHEN $2='closed' THEN now() ELSE NULL END, closed_by=CASE WHEN $2='closed' THEN 'support' ELSE NULL END WHERE id=$1`,
		id, status, in.AdminID, in.AdminName); err != nil {
		ticketErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		ticketErr(w, err)
		return
	}
	if u, err := s.store.UserByID(r.Context(), t.userID); err == nil {
		s.notifyAsync(u, common.NoticeSystem, "客服已回复您的工单", "您的工单「"+t.Subject+"」有新的客服回复，请前往 帮助与客服 → 我的工单 查看。")
	}
	t, err = s.store.GetTicket(r.Context(), id)
	if err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ticket": t, "before": map[string]string{"status": before}})
}

// PATCH /internal/admin/support/tickets/{id} {status?, assigneeId?, assigneeName?, adminName}
// assigneeId 0 unassigns; omit to leave unchanged.
func (s *Server) handleInternalPatchTicket(w http.ResponseWriter, r *http.Request) {
	if !s.internalOnly(w, r) {
		return
	}
	var in struct {
		Status       *string `json:"status"`
		AssigneeID   *int64  `json:"assigneeId"`
		AssigneeName string  `json:"assigneeName"`
		AdminName    string  `json:"adminName"`
	}
	if err := common.DecodeJSON(r, &in); err != nil || (in.Status == nil && in.AssigneeID == nil) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请指定 status 或 assigneeId")
		return
	}
	if in.Status != nil && *in.Status != ticketStatusOpen && *in.Status != ticketStatusReply && *in.Status != ticketStatusClose {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的状态")
		return
	}
	id, t, ok := s.internalTicket(w, r)
	if !ok {
		return
	}
	before := map[string]any{"status": t.Status, "assigneeId": t.AssigneeID, "assigneeName": t.AssigneeName}
	if in.Status != nil {
		if _, err := s.store.db.ExecContext(r.Context(), `UPDATE support_tickets SET status=$2, updated_at=now(),
			closed_at=CASE WHEN $2='closed' THEN COALESCE(closed_at, now()) ELSE NULL END,
			closed_by=CASE WHEN $2='closed' THEN COALESCE(closed_by, $3) ELSE NULL END WHERE id=$1`, id, *in.Status, "support:"+in.AdminName); err != nil {
			ticketErr(w, err)
			return
		}
	}
	if in.AssigneeID != nil {
		var aid, aname any
		if *in.AssigneeID > 0 {
			aid, aname = *in.AssigneeID, in.AssigneeName
		}
		if _, err := s.store.db.ExecContext(r.Context(), `UPDATE support_tickets SET assignee_id=$2, assignee_name=$3, updated_at=now() WHERE id=$1`, id, aid, aname); err != nil {
			ticketErr(w, err)
			return
		}
	}
	t, err := s.store.GetTicket(r.Context(), id)
	if err != nil {
		ticketErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ticket": t, "before": before})
}

// GET /internal/admin/support/attachments/{id}
func (s *Server) handleInternalTicketAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.internalOnly(w, r) {
		return
	}
	s.serveAttachment(w, r, 0)
}

func mustID(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
