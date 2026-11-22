package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
)

// cs.go: 1:1 customer-service (客服) conversations. A user (gateway X-User-Id)
// or an anonymous visitor (guest_key, gateway-allow-listed /cs/guest/*) opens a
// conversation; staff reply via the admin workbench (csadmin.go). Live updates
// are pushed on the WS channel "cs:{pubToken}" (unguessable token => the public
// /ws needs no per-user auth). History is fetched over REST with ?after=.

type csConversation struct {
	ID        int64     `json:"id"`
	PubToken  string    `json:"pubToken"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	Assignee  string    `json:"assignee,omitempty"`
	LastMsgAt time.Time `json:"lastMsgAt"`
	Unread    int       `json:"unread"`
	CreatedAt time.Time `json:"createdAt"`
}

type csMessage struct {
	ID         int64     `json:"id"`
	ConvID     int64     `json:"convId"`
	SenderKind string    `json:"senderKind"`
	Sender     string    `json:"sender,omitempty"`
	Kind       string    `json:"kind"`
	Text       string    `json:"text,omitempty"`
	ImageURL   string    `json:"imageUrl,omitempty"`
	ClientID   string    `json:"clientId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// CS holds customer-service state.
type CS struct {
	srv  *Server
	chat *Chat // reused for config (filter rules, max length, rate interval)

	gmu       sync.Mutex
	guestHits map[string][]time.Time // "ip|guestKey" -> recent send timestamps
}

func newCS(s *Server, chat *Chat) *CS {
	return &CS{srv: s, chat: chat, guestHits: map[string][]time.Time{}}
}

func newPubToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- display ----

// publicSender hides identities on the user/guest-facing surface; the client
// distinguishes its own messages by senderKind=="user".
func publicSender(kind string) string {
	switch kind {
	case "staff":
		return "客服"
	case "system":
		return "系统"
	default:
		return ""
	}
}

// ---- config helpers (reuse chat config) ----

func (c *CS) enabled() bool { return c.srv.cfg.CSEnabled }

// ---- conversation lookups ----

func (c *CS) convByID(ctx context.Context, id int64) (*csConversation, string, string, error) {
	var cv csConversation
	var uid, guest string
	err := c.srv.store.db.QueryRowContext(ctx,
		`SELECT id, pub_token, subject, status, assignee, last_msg_at, created_at, uid, guest_key, unread_user
		 FROM cs_conversations WHERE id=$1`, id).
		Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &uid, &guest, &cv.Unread)
	if err != nil {
		return nil, "", "", err
	}
	return &cv, uid, guest, nil
}

func (c *CS) convByToken(ctx context.Context, token string) (*csConversation, string, string, error) {
	var cv csConversation
	var uid, guest string
	err := c.srv.store.db.QueryRowContext(ctx,
		`SELECT id, pub_token, subject, status, assignee, last_msg_at, created_at, uid, guest_key, unread_user
		 FROM cs_conversations WHERE pub_token=$1`, token).
		Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &uid, &guest, &cv.Unread)
	if err != nil {
		return nil, "", "", err
	}
	return &cv, uid, guest, nil
}

// ---- message insert (idempotent) ----

// replay returns the message this sender already stored under clientID, if
// any. Callers check it BEFORE validation and rate limits so a client retry
// of an accepted send gets 200 + the original message instead of 429.
func (c *CS) replay(ctx context.Context, sender, clientID string) (csMessage, bool, error) {
	var m csMessage
	if clientID == "" {
		return m, false, nil
	}
	err := c.srv.store.db.QueryRowContext(ctx,
		`SELECT id, conv_id, sender_kind, client_id, kind, text, image_url, created_at
		 FROM cs_messages WHERE sender=$1 AND client_id=$2`, sender, clientID).
		Scan(&m.ID, &m.ConvID, &m.SenderKind, &m.ClientID, &m.Kind, &m.Text, &m.ImageURL, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m.Sender = publicSender(m.SenderKind)
	return m, true, nil
}

func (c *CS) insert(ctx context.Context, convID int64, sender, senderKind, clientID, kind, text, imageURL string) (csMessage, bool, error) {
	var m csMessage
	m.ConvID, m.SenderKind, m.Kind, m.ClientID = convID, senderKind, kind, clientID
	if prev, ok, err := c.replay(ctx, sender, clientID); err != nil {
		return m, false, err
	} else if ok {
		return prev, true, nil
	}
	err := c.srv.store.db.QueryRowContext(ctx,
		`INSERT INTO cs_messages (conv_id, sender, sender_kind, client_id, kind, text, image_url)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (sender, client_id) WHERE client_id <> '' DO NOTHING
		 RETURNING id, created_at`, convID, sender, senderKind, clientID, kind, text, imageURL).
		Scan(&m.ID, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) { // lost race on same clientId
		_ = c.srv.store.db.QueryRowContext(ctx,
			`SELECT id, kind, text, image_url, created_at FROM cs_messages WHERE sender=$1 AND client_id=$2`, sender, clientID).
			Scan(&m.ID, &m.Kind, &m.Text, &m.ImageURL, &m.CreatedAt)
		m.Sender = publicSender(senderKind)
		return m, true, nil
	}
	if err != nil {
		return m, false, err
	}
	m.Text, m.ImageURL, m.Sender = text, imageURL, publicSender(senderKind)
	return m, false, nil
}

// bump updates the conversation's activity + unread counters after a message.
// userMsg=true means the sender was the user/guest (staff gains unread),
// otherwise a staff reply (user gains unread).
func (c *CS) bump(ctx context.Context, convID int64, userMsg bool) {
	if userMsg {
		_, _ = c.srv.store.db.ExecContext(ctx,
			`UPDATE cs_conversations SET last_msg_at=now(), updated_at=now(), unread_staff=unread_staff+1,
			 status=CASE WHEN status IN ('resolved','closed') THEN 'open' ELSE status END WHERE id=$1`, convID)
	} else {
		_, _ = c.srv.store.db.ExecContext(ctx,
			`UPDATE cs_conversations SET last_msg_at=now(), updated_at=now(), unread_user=unread_user+1 WHERE id=$1`, convID)
	}
}

func (c *CS) broadcast(token string, m csMessage) {
	c.srv.hub.Broadcast("cs:"+token, map[string]any{"type": "message", "message": m})
}

// ---- text validation (reuse chat filter/limits) ----

func (c *CS) validateText(ctx context.Context, text string) (clean string, code, msg string) {
	cfg := c.chat.config(ctx)
	clean = cleanChatText(text)
	if clean == "" {
		return "", "VALIDATION_ERROR", "消息不能为空"
	}
	if utf8.RuneCountInString(clean) > cfg.MaxLength {
		return "", "CS_TOO_LONG", "消息不能超过 " + strconv.Itoa(cfg.MaxLength) + " 字"
	}
	if code, msg := chatViolation(clean, cfg); code != "" {
		return "", code, msg
	}
	return clean, "", ""
}

// ---- guest anti-abuse (in-memory, per ip|guestKey) ----

func (c *CS) guestAllow(key string) (ok bool, retryMs int64) {
	now := time.Now()
	c.gmu.Lock()
	defer c.gmu.Unlock()
	hits := c.guestHits[key]
	cutoff := now.Add(-time.Hour)
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= c.srv.cfg.GuestMaxPerHour {
		c.guestHits[key] = kept
		return false, 0
	}
	if len(kept) > 0 {
		if wait := time.Duration(c.srv.cfg.GuestMinIntervalMs)*time.Millisecond - now.Sub(kept[len(kept)-1]); wait > 0 {
			return false, wait.Milliseconds()
		}
	}
	kept = append(kept, now)
	c.guestHits[key] = kept
	if len(c.guestHits) > 200000 {
		c.guestHits = map[string][]time.Time{key: kept}
	}
	return true, 0
}

// ipAllow is a coarse per-client-IP cap shared by every guest key from that
// IP (sliding 1-minute window, no min interval so visitors behind one NAT are
// not serialised). Rotating guestKey cannot bypass it.
func (c *CS) ipAllow(ip string) bool {
	max := c.srv.cfg.GuestIPMaxPerMin
	if max <= 0 || ip == "" {
		return true
	}
	now := time.Now()
	key := "ip#" + ip
	c.gmu.Lock()
	defer c.gmu.Unlock()
	hits := c.guestHits[key]
	cutoff := now.Add(-time.Minute)
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= max {
		c.guestHits[key] = kept
		return false
	}
	c.guestHits[key] = append(kept, now)
	return true
}

// guestLimit applies the per-IP cap, then the per ip|guestKey interval/hourly
// cap. Writes the 429 and returns false when either trips.
func (c *CS) guestLimit(w http.ResponseWriter, r *http.Request, guestKey, msg string) bool {
	ip := clientIP(r)
	if !c.ipAllow(ip) {
		w.Header().Set("Retry-After", "60")
		common.WriteError(w, http.StatusTooManyRequests, "CS_RATE_LIMITED", msg)
		return false
	}
	if ok, retry := c.guestAllow(ip + "|" + guestKey); !ok {
		if retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry/1000)+1))
		}
		common.WriteError(w, http.StatusTooManyRequests, "CS_RATE_LIMITED", msg)
		return false
	}
	return true
}

// ---- user handlers ----

func (c *CS) gate(w http.ResponseWriter) bool {
	if !c.enabled() {
		common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "客服即将上线")
		return false
	}
	return true
}

func (c *CS) uid(r *http.Request) string { return r.Header.Get("X-User-Id") }

// POST /cs/conversations {subject?} -> get-or-create the user's open conversation.
func (c *CS) hOpen(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	uid := c.uid(r)
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var in struct {
		Subject string `json:"subject"`
	}
	if r.ContentLength != 0 {
		_ = common.DecodeJSON(r, &in)
	}
	ctx := r.Context()
	var cv csConversation
	err := c.srv.store.db.QueryRowContext(ctx,
		`SELECT id, pub_token, subject, status, assignee, last_msg_at, created_at, unread_user
		 FROM cs_conversations WHERE uid=$1 AND status NOT IN ('closed') ORDER BY last_msg_at DESC LIMIT 1`, uid).
		Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &cv.Unread)
	if errors.Is(err, sql.ErrNoRows) {
		subj := strings.TrimSpace(in.Subject)
		if utf8.RuneCountInString(subj) > 100 {
			subj = string([]rune(subj)[:100])
		}
		tok := newPubToken()
		if err := c.srv.store.db.QueryRowContext(ctx,
			`INSERT INTO cs_conversations (pub_token, uid, ip, subject) VALUES ($1,$2,$3,$4)
			 RETURNING id, pub_token, subject, status, assignee, last_msg_at, created_at, unread_user`,
			tok, uid, clientIP(r), subj).
			Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &cv.Unread); err != nil {
			intErr(w, "cs open", err)
			return
		}
	} else if err != nil {
		intErr(w, "cs open", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, cv)
}

// GET /cs/conversations -> the user's conversations (newest first).
func (c *CS) hList(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	uid := c.uid(r)
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	rows, err := c.srv.store.db.QueryContext(r.Context(),
		`SELECT id, pub_token, subject, status, assignee, last_msg_at, created_at, unread_user
		 FROM cs_conversations WHERE uid=$1 ORDER BY last_msg_at DESC LIMIT 100`, uid)
	if err != nil {
		intErr(w, "cs list", err)
		return
	}
	defer rows.Close()
	items := []csConversation{}
	for rows.Next() {
		var cv csConversation
		if err := rows.Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &cv.Unread); err != nil {
			intErr(w, "cs list", err)
			return
		}
		items = append(items, cv)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// messages reads a conversation's messages (ascending), used by owner + guest.
func (c *CS) messages(ctx context.Context, convID int64, after int64, limit int) ([]csMessage, error) {
	rows, err := c.srv.store.db.QueryContext(ctx,
		`SELECT id, conv_id, sender_kind, kind, text, image_url, created_at
		 FROM cs_messages WHERE conv_id=$1 AND status='visible' AND id > $2
		 ORDER BY id ASC LIMIT $3`, convID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []csMessage{}
	for rows.Next() {
		var m csMessage
		if err := rows.Scan(&m.ID, &m.ConvID, &m.SenderKind, &m.Kind, &m.Text, &m.ImageURL, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Sender = publicSender(m.SenderKind)
		out = append(out, m)
	}
	return out, rows.Err()
}

// GET /cs/conversations/{id}/messages?after=&limit= (owner); clears user unread.
func (c *CS) hMessages(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	uid := c.uid(r)
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	cv, owner, _, err := c.convByID(r.Context(), id)
	if err != nil || owner != uid {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	after := int64(atoiDefault(r.URL.Query().Get("after"), 0, 0, 1<<62))
	limit := atoiDefault(r.URL.Query().Get("limit"), 50, 1, 200)
	items, err := c.messages(r.Context(), cv.ID, after, limit)
	if err != nil {
		intErr(w, "cs messages", err)
		return
	}
	_, _ = c.srv.store.db.ExecContext(r.Context(), `UPDATE cs_conversations SET unread_user=0 WHERE id=$1`, cv.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "pubToken": cv.PubToken})
}

// POST /cs/conversations/{id}/read -> clear the user's unread counter.
func (c *CS) hRead(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	uid := c.uid(r)
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	res, err := c.srv.store.db.ExecContext(r.Context(), `UPDATE cs_conversations SET unread_user=0 WHERE id=$1 AND uid=$2`, id, uid)
	if err != nil {
		intErr(w, "cs read", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type csSendIn struct {
	ClientID string `json:"clientId"`
	Text     string `json:"text"`
	ImageURL string `json:"imageUrl"`
}

func validImageURL(u string) bool { return strings.HasPrefix(u, "/cs/uploads/") }

// POST /cs/conversations/{id}/messages (owner).
func (c *CS) hSend(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	uid := c.uid(r)
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	cv, owner, _, err := c.convByID(r.Context(), id)
	if err != nil || owner != uid {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	var in csSendIn
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.ClientID == "" || len(in.ClientID) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "clientId 必填且不超过 64 字符")
		return
	}
	c.sendUserMessage(w, r, cv, uid, in)
}

// sendUserMessage is shared by the authed owner and guest send paths.
func (c *CS) sendUserMessage(w http.ResponseWriter, r *http.Request, cv *csConversation, sender string, in csSendIn) {
	ctx := r.Context()
	// Idempotent replay first: same sender+clientId -> 200 original message,
	// regardless of rate limits or later config changes.
	if prev, ok, err := c.replay(ctx, sender, in.ClientID); err != nil {
		intErr(w, "cs send replay", err)
		return
	} else if ok {
		common.WriteJSON(w, http.StatusOK, prev)
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
		clean, code, msg := c.validateText(ctx, in.Text)
		if code != "" {
			common.WriteError(w, http.StatusBadRequest, code, msg)
			return
		}
		text = clean
	}
	// Rate limit by DB last-message time for this sender.
	cfg := c.chat.config(ctx)
	if cfg.MinIntervalMs > 0 {
		var last sql.NullTime
		_ = c.srv.store.db.QueryRowContext(ctx, `SELECT max(created_at) FROM cs_messages WHERE sender=$1`, sender).Scan(&last)
		if last.Valid {
			if wait := time.Duration(cfg.MinIntervalMs)*time.Millisecond - time.Since(last.Time); wait > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
				common.WriteJSON(w, http.StatusTooManyRequests, map[string]any{"code": "CS_RATE_LIMITED",
					"message": "发送太快，请稍后再试", "retryAfterMs": wait.Milliseconds(), "requestId": common.RequestID()})
				return
			}
		}
	}
	m, replayed, err := c.insert(ctx, cv.ID, sender, "user", in.ClientID, kind, text, imageURL)
	if err != nil {
		intErr(w, "cs send", err)
		return
	}
	if replayed {
		common.WriteJSON(w, http.StatusOK, m)
		return
	}
	c.bump(ctx, cv.ID, true)
	c.broadcast(cv.PubToken, m)
	common.WriteJSON(w, http.StatusCreated, m)
}

// ---- guest handlers ----

func (c *CS) guestGate(w http.ResponseWriter) bool {
	if !c.gate(w) {
		return false
	}
	if !c.srv.cfg.CSGuestEnabled {
		common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "访客客服未开放")
		return false
	}
	return true
}

func guestKeyOK(k string) bool { return len(k) >= 8 && len(k) <= 80 }

// POST /cs/guest/conversations {guestKey, subject?}.
func (c *CS) hGuestOpen(w http.ResponseWriter, r *http.Request) {
	if !c.guestGate(w) {
		return
	}
	var in struct {
		GuestKey string `json:"guestKey"`
		Subject  string `json:"subject"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.GuestKey = strings.TrimSpace(in.GuestKey)
	if !guestKeyOK(in.GuestKey) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "guestKey 不合法")
		return
	}
	if !c.guestLimit(w, r, in.GuestKey, "操作过于频繁，请稍后再试") {
		return
	}
	ctx := r.Context()
	var cv csConversation
	err := c.srv.store.db.QueryRowContext(ctx,
		`SELECT id, pub_token, subject, status, assignee, last_msg_at, created_at, unread_user
		 FROM cs_conversations WHERE guest_key=$1 AND uid='' AND status NOT IN ('closed') ORDER BY last_msg_at DESC LIMIT 1`, in.GuestKey).
		Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &cv.Unread)
	if errors.Is(err, sql.ErrNoRows) {
		subj := strings.TrimSpace(in.Subject)
		if utf8.RuneCountInString(subj) > 100 {
			subj = string([]rune(subj)[:100])
		}
		if err := c.srv.store.db.QueryRowContext(ctx,
			`INSERT INTO cs_conversations (pub_token, guest_key, ip, subject) VALUES ($1,$2,$3,$4)
			 RETURNING id, pub_token, subject, status, assignee, last_msg_at, created_at, unread_user`,
			newPubToken(), in.GuestKey, clientIP(r), subj).
			Scan(&cv.ID, &cv.PubToken, &cv.Subject, &cv.Status, &cv.Assignee, &cv.LastMsgAt, &cv.CreatedAt, &cv.Unread); err != nil {
			intErr(w, "cs guest open", err)
			return
		}
	} else if err != nil {
		intErr(w, "cs guest open", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, cv)
}

// GET /cs/guest/conversations/{token}/messages?after=&limit= (possession-based).
func (c *CS) hGuestMessages(w http.ResponseWriter, r *http.Request) {
	if !c.guestGate(w) {
		return
	}
	token := r.PathValue("token")
	cv, _, _, err := c.convByToken(r.Context(), token)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	after := int64(atoiDefault(r.URL.Query().Get("after"), 0, 0, 1<<62))
	limit := atoiDefault(r.URL.Query().Get("limit"), 50, 1, 200)
	items, err := c.messages(r.Context(), cv.ID, after, limit)
	if err != nil {
		intErr(w, "cs guest messages", err)
		return
	}
	_, _ = c.srv.store.db.ExecContext(r.Context(), `UPDATE cs_conversations SET unread_user=0 WHERE id=$1`, cv.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /cs/guest/conversations/{token}/messages {guestKey, clientId, text|imageUrl}.
func (c *CS) hGuestSend(w http.ResponseWriter, r *http.Request) {
	if !c.guestGate(w) {
		return
	}
	token := r.PathValue("token")
	var in struct {
		GuestKey string `json:"guestKey"`
		ClientID string `json:"clientId"`
		Text     string `json:"text"`
		ImageURL string `json:"imageUrl"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.GuestKey = strings.TrimSpace(in.GuestKey)
	in.ClientID = strings.TrimSpace(in.ClientID)
	if !guestKeyOK(in.GuestKey) || in.ClientID == "" || len(in.ClientID) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "参数不合法")
		return
	}
	cv, _, guest, err := c.convByToken(r.Context(), token)
	if err != nil || guest != in.GuestKey {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	// Idempotent replay before the guest limiters (a retry is not abuse).
	if prev, ok, err := c.replay(r.Context(), "guest:"+in.GuestKey, in.ClientID); err != nil {
		intErr(w, "cs guest replay", err)
		return
	} else if ok {
		common.WriteJSON(w, http.StatusOK, prev)
		return
	}
	if !c.guestLimit(w, r, in.GuestKey, "发送过于频繁，请稍后再试") {
		return
	}
	// Guest sender id is namespaced so it can never collide with a real uid.
	c.sendUserMessage(w, r, cv, "guest:"+in.GuestKey, csSendIn{ClientID: in.ClientID, Text: in.Text, ImageURL: in.ImageURL})
}

// POST /cs/conversations/claim {guestKey} -> attach the visitor's open guest
// conversations to the now-logged-in user.
func (c *CS) hClaim(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	uid := c.uid(r)
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var in struct {
		GuestKey string `json:"guestKey"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.GuestKey = strings.TrimSpace(in.GuestKey)
	if !guestKeyOK(in.GuestKey) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "guestKey 不合法")
		return
	}
	res, err := c.srv.store.db.ExecContext(r.Context(),
		`UPDATE cs_conversations SET uid=$1, guest_key='', updated_at=now() WHERE guest_key=$2 AND uid=''`, uid, in.GuestKey)
	if err != nil {
		intErr(w, "cs claim", err)
		return
	}
	n, _ := res.RowsAffected()
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "claimed": n})
}

// ---- image upload (optional) ----

var allowedImageExt = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
}

func (c *CS) hUpload(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w) {
		return
	}
	// Either an authed user or a guest (gateway allow-lists /cs/guest/uploads).
	r.Body = http.MaxBytesReader(w, r.Body, c.srv.cfg.UploadMaxBytes+1024)
	if err := r.ParseMultipartForm(c.srv.cfg.UploadMaxBytes + 1024); err != nil {
		common.WriteError(w, http.StatusBadRequest, "CS_UPLOAD_TOO_LARGE", "图片过大或格式错误")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 file")
		return
	}
	defer f.Close()
	if hdr.Size > c.srv.cfg.UploadMaxBytes {
		common.WriteError(w, http.StatusBadRequest, "CS_UPLOAD_TOO_LARGE", "图片过大")
		return
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	ct := http.DetectContentType(head[:n])
	ext, ok := allowedImageExt[ct]
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "CS_UPLOAD_TYPE", "仅支持 jpg/png/webp/gif")
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		intErr(w, "cs upload seek", err)
		return
	}
	name := newPubToken() + ext
	if err := saveUpload(c.srv.cfg.UploadDir, name, f, c.srv.cfg.UploadMaxBytes); err != nil {
		intErr(w, "cs upload save", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"imageUrl": "/cs/uploads/" + name})
}

// GET /cs/uploads/{name} serves a stored image.
func (c *CS) hServeUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, "/\\.") && filepath.Ext(name) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "文件名不合法")
		return
	}
	// Prevent traversal: only allow a bare base name.
	if filepath.Base(name) != name {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "文件名不合法")
		return
	}
	http.ServeFile(w, r, filepath.Join(c.srv.cfg.UploadDir, name))
}

func (c *CS) validCSChannel(token string) (string, string) {
	if !c.enabled() {
		return "FEATURE_DISABLED", "客服即将上线"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, _, err := c.convByToken(ctx, token); err != nil {
		return "SUB_INVALID", "会话不存在"
	}
	return "", ""
}

func (s *Server) csRoutes(mux *http.ServeMux) {
	c := newCS(s, s.chat)
	s.cs = c
	s.hub.csValidate = c.validCSChannel
	// user (authed)
	mux.HandleFunc("POST /cs/conversations", c.hOpen)
	mux.HandleFunc("GET /cs/conversations", c.hList)
	mux.HandleFunc("POST /cs/conversations/claim", c.hClaim)
	mux.HandleFunc("GET /cs/conversations/{id}/messages", c.hMessages)
	mux.HandleFunc("POST /cs/conversations/{id}/messages", c.hSend)
	mux.HandleFunc("POST /cs/conversations/{id}/read", c.hRead)
	mux.HandleFunc("POST /cs/uploads", c.hUpload)
	mux.HandleFunc("GET /cs/uploads/{name}", c.hServeUpload)
	// guest (gateway allow-lists /cs/guest/*)
	mux.HandleFunc("POST /cs/guest/conversations", c.hGuestOpen)
	mux.HandleFunc("GET /cs/guest/conversations/{token}/messages", c.hGuestMessages)
	mux.HandleFunc("POST /cs/guest/conversations/{token}/messages", c.hGuestSend)
	mux.HandleFunc("POST /cs/guest/uploads", c.hUpload)
	// admin workbench
	c.adminRoutes(mux)
}
