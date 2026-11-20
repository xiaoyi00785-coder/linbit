package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// sendMessageReq is the JSON body for POST message endpoints. Tencent IM names
// are accepted so the Flutter SDK can post unchanged.
type sendMessageReq struct {
	MsgRandom int64           `json:"msgRandom,omitempty"`
	ClientSeq int64           `json:"clientSeq,omitempty"`
	Kind      string          `json:"kind,omitempty"`
	Type      string          `json:"type,omitempty"`    // tencent alias
	Payload   json.RawMessage `json:"payload,omitempty"` // native
	MsgBody   json.RawMessage `json:"msgBody,omitempty"` // tencent alias
	Mentions  []string        `json:"mentions,omitempty"`
	ReplyTo   int64           `json:"replyTo,omitempty"`
}

var validKinds = map[string]bool{
	"text": true, "image": true, "audio": true, "video": true, "file": true,
	"custom": true, "system": true, "redpacket": true, "tip": true, "location": true,
}

// POST /im/conversations/{peer}/messages
func (s *Server) handleSendC2C(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	peer := peerPath(w, r.PathValue("peer"))
	if peer == "" {
		return
	}
	if peer == uid {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不能给自己发送消息")
		return
	}
	req, ok := decodeSend(w, r)
	if !ok {
		return
	}
	conv, err := s.store.upsertC2C(r.Context(), uid, peer)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "会话创建失败")
		return
	}
	m, err := s.saveAndFan(r.Context(), conv.ID, uid, req)
	if err != nil {
		writeSendErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, messageDTO(m, conv))
}

// GET /im/conversations/{peer}/messages?beforeSeq=&limit=
func (s *Server) handleListC2C(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	peer := peerPath(w, r.PathValue("peer"))
	if peer == "" {
		return
	}
	conv, err := s.store.upsertC2C(r.Context(), uid, peer)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "会话查询失败")
		return
	}
	s.writeHistory(w, r, conv)
}

// POST /im/groups/{id}/messages
func (s *Server) handleSendGroup(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	gid := pathID(w, r.PathValue("id"))
	if gid == 0 {
		return
	}
	g, err := s.store.getGroup(r.Context(), gid)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群不存在")
		return
	}
	m, err := s.store.getGroupMember(r.Context(), gid, uid)
	if err != nil {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非群成员")
		return
	}
	if g.MuteAll && m.Role == "member" {
		common.WriteError(w, http.StatusForbidden, "GROUP_MUTED", "群已禁言")
		return
	}
	if m.MutedUntil.Valid && m.MutedUntil.Time.After(s.now()) {
		common.WriteError(w, http.StatusForbidden, "USER_MUTED", "你已被禁言")
		return
	}
	req, ok := decodeSend(w, r)
	if !ok {
		return
	}
	convID, _ := s.store.groupConvID(r.Context(), gid)
	if convID == 0 {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "群会话丢失")
		return
	}
	msg, err := s.saveAndFan(r.Context(), convID, uid, req)
	if err != nil {
		writeSendErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, messageDTO(msg, &Conversation{ID: convID, Kind: "group", GroupID: sql.NullInt64{Int64: gid, Valid: true}}))
}

// GET /im/groups/{id}/messages?beforeSeq=&limit=
func (s *Server) handleListGroupMessages(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	gid := pathID(w, r.PathValue("id"))
	if gid == 0 {
		return
	}
	if _, err := s.store.getGroupMember(r.Context(), gid, uid); err != nil {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非群成员")
		return
	}
	convID, err := s.store.groupConvID(r.Context(), gid)
	if err != nil || convID == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群会话不存在")
		return
	}
	s.writeHistory(w, r, &Conversation{ID: convID, Kind: "group", GroupID: sql.NullInt64{Int64: gid, Valid: true}})
}

// POST /im/messages/{id}/recall
func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	m, err := s.store.getMessage(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "消息不存在")
		return
	}
	if m.SenderUID != uid {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "只能撤回自己的消息")
		return
	}
	if s.now().Sub(m.CreatedAt) > s.cfg.RecallWindow {
		common.WriteError(w, http.StatusBadRequest, "RECALL_EXPIRED", "超出可撤回时间窗口")
		return
	}
	if err := s.store.recallMessage(r.Context(), id); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "撤回失败")
		return
	}
	s.hub.broadcastConversation(m.ConversationID, map[string]any{
		"event":          "recall",
		"messageId":      m.ID,
		"MsgSeq":         m.Seq,
		"conversationId": m.ConversationID,
		"ts":             s.nowUnixMilli(),
	})
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "messageId": m.ID})
}

// POST /im/messages/{id}/read
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	m, err := s.store.getMessage(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "消息不存在")
		return
	}
	if err := s.store.markRead(r.Context(), m.ConversationID, uid, m.Seq); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "回执失败")
		return
	}
	// Notify the sender (and other conversation members) that uid has read up
	// to this seq. The sender's UI uses it to flip the ✓ into ✓✓.
	s.hub.broadcastConversation(m.ConversationID, map[string]any{
		"event":          "read",
		"byUserId":       uid,
		"MsgSeq":         m.Seq,
		"conversationId": m.ConversationID,
		"ts":             s.nowUnixMilli(),
	})
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /im/messages/{id}/forward {toPeer?, toGroup?}
func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	uid := s.uid(r)
	id := pathID(w, r.PathValue("id"))
	if id == 0 {
		return
	}
	src, err := s.store.getMessage(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "消息不存在")
		return
	}
	var body struct {
		ToPeer  string `json:"toPeer,omitempty"`
		ToGroup int64  `json:"toGroup,omitempty"`
	}
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if (body.ToPeer == "") == (body.ToGroup == 0) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "必须指定 toPeer 或 toGroup 之一")
		return
	}
	var convID int64
	if body.ToPeer != "" {
		conv, err := s.store.upsertC2C(r.Context(), uid, body.ToPeer)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "目标会话异常")
			return
		}
		convID = conv.ID
	} else {
		if _, err := s.store.getGroupMember(r.Context(), body.ToGroup, uid); err != nil {
			common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "非目标群成员")
			return
		}
		convID, _ = s.store.groupConvID(r.Context(), body.ToGroup)
	}
	req := sendMessageReq{
		Kind:      src.Kind,
		Payload:   src.Payload,
		Mentions:  nil,
		MsgRandom: randomID(),
	}
	m, err := s.saveAndFan(r.Context(), convID, uid, req)
	if err != nil {
		writeSendErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, messageDTO(m, &Conversation{ID: convID}))
}

// saveAndFan inserts the message, bumps the conversation and pushes it through
// the hub.
func (s *Server) saveAndFan(ctx context.Context, convID int64, senderUID string, req sendMessageReq) (*Message, error) {
	kind := req.Kind
	if kind == "" {
		kind = req.Type
	}
	if kind == "" {
		kind = "text"
	}
	if !validKinds[kind] {
		return nil, errBadKind
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = req.MsgBody
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	rand := req.MsgRandom
	if rand == 0 {
		rand = randomID()
	}
	var reply sql.NullInt64
	if req.ReplyTo > 0 {
		reply = sql.NullInt64{Int64: req.ReplyTo, Valid: true}
	}
	m, err := s.store.insertMessage(ctx, convID, senderUID, kind, payload, req.Mentions, reply, rand)
	if err != nil {
		return nil, err
	}
	s.hub.broadcastConversation(convID, map[string]any{
		"event":   "message",
		"message": messageDTO(m, &Conversation{ID: convID}),
	})
	return m, nil
}

// decodeSend reads and validates the shared send body.
func decodeSend(w http.ResponseWriter, r *http.Request) (sendMessageReq, bool) {
	var req sendMessageReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return req, false
	}
	return req, true
}

// writeHistory emits the message list for a conversation; this is the shared
// implementation behind the 1v1 and group history handlers.
func (s *Server) writeHistory(w http.ResponseWriter, r *http.Request, conv *Conversation) {
	beforeSeq := int64(0)
	if v := r.URL.Query().Get("beforeSeq"); v != "" {
		if n, err := parseInt64(v); err == nil {
			beforeSeq = n
		}
	}
	limit := queryLimit(r, 30, 200)
	list, err := s.store.listMessages(r.Context(), conv.ID, beforeSeq, limit)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "历史消息加载失败")
		return
	}
	dtos := make([]map[string]any, 0, len(list))
	for i := range list {
		dtos = append(dtos, messageDTO(&list[i], conv))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"MsgList":        dtos,
		"items":          dtos,
		"conversationId": conv.ID,
	})
}

// messageDTO renders a message in the Tencent-IM-flavoured shape the Flutter
// SDK expects. Fields are duplicated under both native (camelCase) and legacy
// (MsgList-style) names so either side of the wire stays happy.
func messageDTO(m *Message, conv *Conversation) map[string]any {
	kind := m.Kind
	var payload any
	_ = json.Unmarshal(m.Payload, &payload)
	out := map[string]any{
		"MsgSeq":          m.Seq,
		"MsgRandom":       m.MsgRandom,
		"MsgTimeStamp":    m.CreatedAt.Unix(),
		"MsgTime":         m.CreatedAt.UnixMilli(),
		"From_Account":    m.SenderUID,
		"fromUserId":      m.SenderUID,
		"messageId":       m.ID,
		"id":              m.ID,
		"conversationId":  m.ConversationID,
		"kind":            kind,
		"type":            kind,
		"payload":         payload,
		"MsgBody":         payload,
		"mentions":        m.Mentions,
		"recalled":        m.Recalled,
		"MsgRecalled":     m.Recalled,
	}
	if m.ReplyTo.Valid {
		out["replyTo"] = m.ReplyTo.Int64
	}
	if conv != nil {
		if conv.GroupID.Valid {
			out["GroupId"] = conv.GroupID.Int64
			out["groupId"] = conv.GroupID.Int64
		}
	}
	return out
}

// errBadKind is returned by saveAndFan when the client sent an unknown kind.
var errBadKind = errors.New("unsupported message kind")

// writeSendErr translates send errors into HTTP responses.
func writeSendErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadKind):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不支持的消息类型")
	default:
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "消息保存失败")
	}
}

// randomID returns a positive int63 suitable for the MsgRandom field.
func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := int64(binary.BigEndian.Uint64(b[:]) & 0x7FFFFFFFFFFFFFFF)
	if n == 0 {
		n = time.Now().UnixNano()
	}
	return n
}

// parseInt64 parses a decimal int64 without bringing strconv into every caller.
func parseInt64(v string) (int64, error) {
	v = strings.TrimSpace(v)
	n := int64(0)
	neg := false
	if len(v) > 0 && (v[0] == '-' || v[0] == '+') {
		neg = v[0] == '-'
		v = v[1:]
	}
	if v == "" {
		return 0, errors.New("empty")
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

