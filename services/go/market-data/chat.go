package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
)

// chat.go: trading chat rooms (feature flag `chat`).
//
// Rooms: "general" + "spot.{SYMBOL}" for every listed spot pair +
// "futures.{SYMBOL}" for every futures contract. Anyone may read; posting needs
// a logged-in user (gateway X-User-Id) with kycLevel >= cfg.minKycLevel, not
// muted/banned, within the rate limit and free of links / contact info.
// WS: subscribe "chat:{room}" on /ws; frames data = {type:"message",message} or
// {type:"delete",id}.

// ChatConfig is the admin-tunable chat configuration (stored as JSONB).
type ChatConfig struct {
	MinIntervalMs   int      `json:"minIntervalMs"`
	MaxLength       int      `json:"maxLength"`
	MinKycLevel     int      `json:"minKycLevel"`
	BlockLinks      bool     `json:"blockLinks"`
	BlockContacts   bool     `json:"blockContacts"`
	BannedWords     []string `json:"bannedWords"`
	HistoryLimitMax int      `json:"historyLimitMax"`
	GeneralRoom     bool     `json:"generalRoom"`
	FuturesRooms    bool     `json:"futuresRooms"`
	ReportsToHide   int      `json:"reportsToHide"` // 0 = never auto-hide
}

func defaultChatConfig() ChatConfig {
	return ChatConfig{MinIntervalMs: 3000, MaxLength: 200, MinKycLevel: 1, BlockLinks: true, BlockContacts: true,
		BannedWords: []string{}, HistoryLimitMax: 100, GeneralRoom: true, FuturesRooms: true, ReportsToHide: 0}
}

// validate clamps/rejects config values.
func (c ChatConfig) validate() error {
	switch {
	case c.MinIntervalMs < 0 || c.MinIntervalMs > 3600_000:
		return errors.New("minIntervalMs 须在 0-3600000")
	case c.MaxLength < 1 || c.MaxLength > 2000:
		return errors.New("maxLength 须在 1-2000")
	case c.MinKycLevel < 0 || c.MinKycLevel > 3:
		return errors.New("minKycLevel 须在 0-3")
	case c.HistoryLimitMax < 10 || c.HistoryLimitMax > 500:
		return errors.New("historyLimitMax 须在 10-500")
	case c.ReportsToHide < 0 || c.ReportsToHide > 1000:
		return errors.New("reportsToHide 须在 0-1000")
	case len(c.BannedWords) > 1000:
		return errors.New("bannedWords 过多")
	}
	return nil
}

type chatMessage struct {
	ID        int64     `json:"id"`
	Room      string    `json:"room"`
	Sender    string    `json:"sender"` // masked uid
	Text      string    `json:"text"`
	ClientID  string    `json:"clientId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type chatRoom struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // general | spot | futures
	Symbol string `json:"symbol,omitempty"`
	Name   string `json:"name"`
	Online int    `json:"online"`
}

// Chat holds chat state on the market-data server.
type Chat struct {
	srv  *Server
	http *http.Client

	mu      sync.Mutex
	cfg     ChatConfig
	cfgAt   time.Time
	futs    []string
	futsAt  time.Time
	kyc     map[string]kycEntry
	enabled bool
	enAt    time.Time
}

type kycEntry struct {
	level int
	at    time.Time
}

func newChat(s *Server) *Chat {
	return &Chat{srv: s, http: &http.Client{Timeout: 3 * time.Second}, kyc: map[string]kycEntry{}}
}

func maskUID(uid string) string {
	if len(uid) <= 4 {
		return "用户***" + uid
	}
	return "用户***" + uid[len(uid)-4:]
}

// ---- config / feature / rooms ----

func (c *Chat) config(ctx context.Context) ChatConfig {
	c.mu.Lock()
	if !c.cfgAt.IsZero() && time.Since(c.cfgAt) < 10*time.Second {
		v := c.cfg
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	cfg := defaultChatConfig()
	var raw []byte
	if err := c.srv.store.db.QueryRowContext(ctx, `SELECT cfg FROM chat_config WHERE id=1`).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("chat config: %v", err)
	}
	if cfg.BannedWords == nil {
		cfg.BannedWords = []string{}
	}
	c.mu.Lock()
	c.cfg, c.cfgAt = cfg, time.Now()
	c.mu.Unlock()
	return cfg
}

func (c *Chat) invalidate() {
	c.mu.Lock()
	c.cfgAt, c.enAt = time.Time{}, time.Time{}
	c.mu.Unlock()
}

// featureOn reports the `chat` app feature flag (cached 10s).
func (c *Chat) featureOn(ctx context.Context) bool {
	c.mu.Lock()
	if !c.enAt.IsZero() && time.Since(c.enAt) < 10*time.Second {
		v := c.enabled
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	on := featureDefaults["chat"]
	if over, _, err := c.srv.store.featureOverrides(ctx); err == nil {
		on = mergeFeatures(over)["chat"]
	}
	c.mu.Lock()
	c.enabled, c.enAt = on, time.Now()
	c.mu.Unlock()
	return on
}

// futuresSymbols reads the futures service's contract list (cached 60s).
func (c *Chat) futuresSymbols(ctx context.Context) []string {
	c.mu.Lock()
	if !c.futsAt.IsZero() && time.Since(c.futsAt) < 60*time.Second {
		v := c.futs
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	var out []string
	if base := c.srv.cfg.FuturesURL; base != "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/futures/market/contracts", nil)
		if resp, err := c.http.Do(req); err == nil {
			var body struct {
				Contracts []struct {
					Symbol string `json:"symbol"`
				} `json:"contracts"`
			}
			if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&body) == nil {
				for _, k := range body.Contracts {
					out = append(out, k.Symbol)
				}
			}
			resp.Body.Close()
		} else {
			log.Printf("chat futures contracts: %v", err)
		}
	}
	c.mu.Lock()
	if out == nil && c.futs != nil {
		out = c.futs // keep last good list on error
	}
	c.futs, c.futsAt = out, time.Now()
	c.mu.Unlock()
	return out
}

func (c *Chat) rooms(ctx context.Context) []chatRoom {
	cfg := c.config(ctx)
	var rooms []chatRoom
	if cfg.GeneralRoom {
		rooms = append(rooms, chatRoom{ID: "general", Type: "general", Name: "综合聊天室"})
	}
	if infos, err := c.srv.store.Symbols(ctx); err == nil {
		for _, si := range infos {
			if si.Status == "delisted" {
				continue
			}
			rooms = append(rooms, chatRoom{ID: "spot." + si.Symbol, Type: "spot", Symbol: si.Symbol, Name: si.Base + "/" + si.Quote + " 现货"})
		}
	} else {
		log.Printf("chat rooms symbols: %v", err)
	}
	if cfg.FuturesRooms {
		fs := c.futuresSymbols(ctx)
		sort.Strings(fs)
		for _, sym := range fs {
			rooms = append(rooms, chatRoom{ID: "futures." + sym, Type: "futures", Symbol: sym, Name: sym + " 永续"})
		}
	}
	for i := range rooms {
		rooms[i].Online = c.srv.hub.channelCount("chat:" + rooms[i].ID)
	}
	return rooms
}

func (c *Chat) roomExists(ctx context.Context, id string) bool {
	for _, r := range c.rooms(ctx) {
		if r.ID == id {
			return true
		}
	}
	return false
}

// kycLevel asks the account service (cached: 5 min when verified, 3s otherwise so a fresh KYC pass unlocks quickly).
func (c *Chat) kycLevel(ctx context.Context, uid string, need int) (int, error) {
	c.mu.Lock()
	e, ok := c.kyc[uid]
	c.mu.Unlock()
	if ok && ((e.level >= need && time.Since(e.at) < 5*time.Minute) || time.Since(e.at) < 3*time.Second) {
		return e.level, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.srv.cfg.AccountURL, "/")+"/internal/account/kyc-level?uid="+uid, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Internal-Token", c.srv.cfg.InternalToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, errors.New("account kyc-level status " + strconv.Itoa(resp.StatusCode))
	}
	var body struct {
		Level int `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	c.mu.Lock()
	if len(c.kyc) > 100000 {
		c.kyc = map[string]kycEntry{}
	}
	c.kyc[uid] = kycEntry{level: body.Level, at: time.Now()}
	c.mu.Unlock()
	return body.Level, nil
}

// ---- public handlers ----

func (c *Chat) gate(w http.ResponseWriter, r *http.Request) bool {
	if !c.featureOn(r.Context()) {
		common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "聊天室即将上线")
		return false
	}
	return true
}

func (c *Chat) hRooms(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	cfg := c.config(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"rooms": c.rooms(r.Context()),
		"rules": map[string]any{"maxLength": cfg.MaxLength, "minIntervalMs": cfg.MinIntervalMs, "minKycLevel": cfg.MinKycLevel,
			"blockLinks": cfg.BlockLinks, "blockContacts": cfg.BlockContacts},
	})
}

const chatCols = `id, room, uid, client_id, text, created_at`

func scanChat(rows *sql.Rows) ([]chatMessage, error) {
	out := []chatMessage{}
	for rows.Next() {
		var m chatMessage
		var uid string
		if err := rows.Scan(&m.ID, &m.Room, &uid, &m.ClientID, &m.Text, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Sender = maskUID(uid)
		m.ClientID = ""
		out = append(out, m)
	}
	return out, rows.Err()
}

// GET /chat/rooms/{room}/messages?before=<id>&limit= — newest first.
func (c *Chat) hMessages(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	room := r.PathValue("room")
	if !c.roomExists(r.Context(), room) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "聊天室不存在")
		return
	}
	cfg := c.config(r.Context())
	limit := atoiDefault(r.URL.Query().Get("limit"), 50, 1, cfg.HistoryLimitMax)
	before := int64(atoiDefault(r.URL.Query().Get("before"), 0, 0, 1<<62))
	q := `SELECT ` + chatCols + ` FROM chat_messages WHERE room=$1 AND status='visible'`
	args := []any{room}
	if before > 0 {
		q += ` AND id < $2`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ` + strconv.Itoa(limit+1)
	rows, err := c.srv.store.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		intErr(w, "chat messages", err)
		return
	}
	defer rows.Close()
	items, err := scanChat(rows)
	if err != nil {
		intErr(w, "chat messages", err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

// POST /chat/rooms/{room}/messages {clientId,text}
func (c *Chat) hPost(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	ctx := r.Context()
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	room := r.PathValue("room")
	var in struct {
		ClientID string `json:"clientId"`
		Text     string `json:"text"`
	}
	if err := common.DecodeJSON(r, &in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.ClientID == "" || len(in.ClientID) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "clientId 必填且不超过 64 字符")
		return
	}
	if !c.roomExists(ctx, room) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "聊天室不存在")
		return
	}
	// Idempotent replay of the same clientId.
	var m chatMessage
	err := c.srv.store.db.QueryRowContext(ctx, `SELECT id, room, text, created_at FROM chat_messages WHERE uid=$1 AND client_id=$2`, uid, in.ClientID).
		Scan(&m.ID, &m.Room, &m.Text, &m.CreatedAt)
	if err == nil {
		m.Sender, m.ClientID = maskUID(uid), in.ClientID
		common.WriteJSON(w, http.StatusOK, m)
		return
	}
	cfg := c.config(ctx)
	text := cleanChatText(in.Text)
	if text == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "消息不能为空")
		return
	}
	if utf8.RuneCountInString(text) > cfg.MaxLength {
		common.WriteError(w, http.StatusBadRequest, "CHAT_TOO_LONG", "消息不能超过 "+strconv.Itoa(cfg.MaxLength)+" 字")
		return
	}
	// Sanctions.
	var muted sql.NullTime
	var banned bool
	if err := c.srv.store.db.QueryRowContext(ctx, `SELECT muted_until, banned FROM chat_sanctions WHERE uid=$1`, uid).Scan(&muted, &banned); err != nil && !errors.Is(err, sql.ErrNoRows) {
		intErr(w, "chat sanction", err)
		return
	}
	if banned {
		common.WriteError(w, http.StatusForbidden, "CHAT_BANNED", "你已被禁止在聊天室发言")
		return
	}
	if muted.Valid && muted.Time.After(time.Now()) {
		common.WriteJSON(w, http.StatusForbidden, map[string]any{"code": "CHAT_MUTED", "message": "你已被禁言",
			"mutedUntil": muted.Time.UTC(), "requestId": common.RequestID()})
		return
	}
	if cfg.MinKycLevel > 0 {
		lvl, err := c.kycLevel(ctx, uid, cfg.MinKycLevel)
		if err != nil {
			log.Printf("chat kyc %v", err)
			common.WriteError(w, http.StatusServiceUnavailable, "CHAT_UNAVAILABLE", "暂时无法发言，请稍后重试")
			return
		}
		if lvl < cfg.MinKycLevel {
			common.WriteError(w, http.StatusForbidden, "CHAT_KYC_REQUIRED", "完成身份认证后才能发言")
			return
		}
	}
	if code, msg := chatViolation(text, cfg); code != "" {
		common.WriteError(w, http.StatusBadRequest, code, msg)
		return
	}
	if cfg.MinIntervalMs > 0 {
		var last sql.NullTime
		_ = c.srv.store.db.QueryRowContext(ctx, `SELECT max(created_at) FROM chat_messages WHERE uid=$1`, uid).Scan(&last)
		if last.Valid {
			wait := time.Duration(cfg.MinIntervalMs)*time.Millisecond - time.Since(last.Time)
			if wait > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
				common.WriteJSON(w, http.StatusTooManyRequests, map[string]any{"code": "CHAT_RATE_LIMITED",
					"message": "发言太快，请稍后再试", "retryAfterMs": wait.Milliseconds(), "requestId": common.RequestID()})
				return
			}
		}
	}
	err = c.srv.store.db.QueryRowContext(ctx, `INSERT INTO chat_messages (room, uid, client_id, text) VALUES ($1,$2,$3,$4)
		ON CONFLICT (uid, client_id) DO NOTHING RETURNING id, created_at`, room, uid, in.ClientID, text).Scan(&m.ID, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) { // lost a race with the same clientId
		_ = c.srv.store.db.QueryRowContext(ctx, `SELECT id, room, text, created_at FROM chat_messages WHERE uid=$1 AND client_id=$2`, uid, in.ClientID).
			Scan(&m.ID, &m.Room, &m.Text, &m.CreatedAt)
		m.Sender, m.ClientID = maskUID(uid), in.ClientID
		common.WriteJSON(w, http.StatusOK, m)
		return
	}
	if err != nil {
		intErr(w, "chat insert", err)
		return
	}
	m.Room, m.Text, m.Sender = room, text, maskUID(uid)
	c.srv.hub.Broadcast("chat:"+room, map[string]any{"type": "message", "message": m})
	m.ClientID = in.ClientID
	common.WriteJSON(w, http.StatusCreated, m)
}

// POST /chat/messages/{id}/report {reason}
func (c *Chat) hReport(w http.ResponseWriter, r *http.Request) {
	if !c.gate(w, r) {
		return
	}
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := common.DecodeJSON(r, &in); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) > 200 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "reason 不超过 200 字")
		return
	}
	ctx := r.Context()
	var author, room string
	if err := c.srv.store.db.QueryRowContext(ctx, `SELECT uid, room FROM chat_messages WHERE id=$1 AND status='visible'`, id).Scan(&author, &room); err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "消息不存在")
		return
	}
	if author == uid {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不能举报自己的消息")
		return
	}
	if _, err := c.srv.store.db.ExecContext(ctx, `INSERT INTO chat_reports (message_id, reporter, reason) VALUES ($1,$2,$3)
		ON CONFLICT (message_id, reporter) DO NOTHING`, id, uid, in.Reason); err != nil {
		intErr(w, "chat report", err)
		return
	}
	if cfg := c.config(ctx); cfg.ReportsToHide > 0 {
		var n int
		_ = c.srv.store.db.QueryRowContext(ctx, `SELECT count(*) FROM chat_reports WHERE message_id=$1 AND status='open'`, id).Scan(&n)
		if n >= cfg.ReportsToHide {
			c.deleteMessage(ctx, id, "system", "auto-hidden: reports")
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (c *Chat) deleteMessage(ctx context.Context, id int64, by, reason string) (before *chatMessage, err error) {
	var m chatMessage
	var uid string
	err = c.srv.store.db.QueryRowContext(ctx, `UPDATE chat_messages SET status='deleted', deleted_by=$2, deleted_reason=$3
		WHERE id=$1 AND status='visible' RETURNING id, room, uid, text, created_at`, id, by, reason).Scan(&m.ID, &m.Room, &uid, &m.Text, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.Sender = uid // full uid for audit
	c.srv.hub.Broadcast("chat:"+m.Room, map[string]any{"type": "delete", "id": m.ID})
	return &m, nil
}

// validChatChannel is the hub hook for "chat:{room}" subscriptions.
func (c *Chat) validChatChannel(room string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !c.featureOn(ctx) {
		return "FEATURE_DISABLED", "聊天室即将上线"
	}
	if !c.roomExists(ctx, room) {
		return "SUB_INVALID", "聊天室不存在: " + room
	}
	return "", ""
}

func (s *Server) chatRoutes(mux *http.ServeMux) {
	c := newChat(s)
	s.chat = c
	s.hub.chatValidate = c.validChatChannel
	mux.HandleFunc("GET /chat/rooms", c.hRooms)
	mux.HandleFunc("GET /chat/rooms/{room}/messages", c.hMessages)
	mux.HandleFunc("POST /chat/rooms/{room}/messages", c.hPost)
	mux.HandleFunc("POST /chat/messages/{id}/report", c.hReport)
	c.adminRoutes(mux)
}
