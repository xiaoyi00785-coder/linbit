package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/redis/go-redis/v9"
)

// Server wires configuration, storage and HTTP handlers for the im service.
type Server struct {
	cfg   Config
	store *Store
	rdb   *redis.Client
	hub   *Hub
	http  *http.Client
	now   func() time.Time
}

// NewServer constructs a Server. rdb may be nil (Redis is optional; the
// redpacket claim path falls back to a DB-row transaction when it is).
func NewServer(cfg Config, store *Store, rdb *redis.Client) *Server {
	s := &Server{
		cfg:   cfg,
		store: store,
		rdb:   rdb,
		http:  &http.Client{Timeout: 5 * time.Second},
		now:   time.Now,
	}
	s.hub = NewHub(s)
	return s
}

// Routes returns the im service HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	// Token / profile. Public from the gateway's perspective: the client has
	// already signed in with the normal Linbit JWT, and this endpoint re-signs
	// a short-lived WebSocket credential for the im channel.
	mux.HandleFunc("POST /im/token", s.handleToken)
	mux.HandleFunc("GET /im/users/{uid}/profile", s.handleUserProfile)

	// Conversation list + unread.
	mux.HandleFunc("GET /im/conversations", s.auth(s.handleListConversations))

	// 1v1.
	mux.HandleFunc("POST /im/conversations/{peer}/messages", s.auth(s.handleSendC2C))
	mux.HandleFunc("GET /im/conversations/{peer}/messages", s.auth(s.handleListC2C))

	// Group messages.
	mux.HandleFunc("POST /im/groups/{id}/messages", s.auth(s.handleSendGroup))
	mux.HandleFunc("GET /im/groups/{id}/messages", s.auth(s.handleListGroupMessages))

	// Message operations.
	mux.HandleFunc("POST /im/messages/{id}/recall", s.auth(s.handleRecall))
	mux.HandleFunc("POST /im/messages/{id}/read", s.auth(s.handleRead))
	mux.HandleFunc("POST /im/messages/{id}/forward", s.auth(s.handleForward))

	// Groups.
	mux.HandleFunc("POST /im/groups", s.auth(s.handleCreateGroup))
	mux.HandleFunc("GET /im/groups/{id}", s.auth(s.handleGetGroup))
	mux.HandleFunc("GET /im/groups/{id}/members", s.auth(s.handleListGroupMembers))
	mux.HandleFunc("POST /im/groups/{id}/members", s.auth(s.handleAddMembers))
	mux.HandleFunc("DELETE /im/groups/{id}/members/{uid}", s.auth(s.handleRemoveMember))
	mux.HandleFunc("POST /im/groups/{id}/members/{uid}/role", s.auth(s.handleSetRole))
	mux.HandleFunc("POST /im/groups/{id}/mute", s.auth(s.handleGroupMute))
	mux.HandleFunc("POST /im/groups/{id}/members/{uid}/mute", s.auth(s.handleMemberMute))
	mux.HandleFunc("PUT /im/groups/{id}/announcement", s.auth(s.handleAnnouncement))

	// Red packets.
	mux.HandleFunc("POST /im/redpackets", s.auth(s.handleCreateRedPacket))
	mux.HandleFunc("GET /im/redpackets/{id}", s.auth(s.handleGetRedPacket))
	mux.HandleFunc("POST /im/redpackets/{id}/claim", s.auth(s.handleClaimRedPacket))
	mux.HandleFunc("GET /im/redpackets/{id}/claims", s.auth(s.handleListClaims))

	// Lottery hook: a group raffle. Thin wrapper on top of the redpacket path
	// so the EasiCoin "转盘/抽奖" stays wired to the same ledger primitive.
	mux.HandleFunc("POST /im/groups/{id}/lottery", s.auth(s.handleGroupLottery))

	// WebSocket. The gateway proxies /im/ws through to this handler.
	mux.HandleFunc("GET /im/ws", s.hub.ServeHTTP)

	return mux
}

// auth requires a gateway-authenticated request (X-User-Id present) OR, for
// routes reached through the compat proxy that doesn't validate, a valid IM
// token in the Authorization header. The resolved uid is stashed in the
// request context under ctxUID for handlers.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-Id")
		if uid == "" {
			if claims, err := s.parseBearer(r); err == nil {
				uid = claims.Subject
			}
		}
		if uid == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUID, uid)
		next(w, r.WithContext(ctx))
	}
}

// ctxKey is the context key type for request-scoped values.
type ctxKey string

const ctxUID ctxKey = "uid"

// uidOf returns the resolved uid for a request processed by the auth middleware.
// Handlers should not call it directly; prefer the s.uid helper below.
func (s *Server) uid(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUID).(string); ok {
		return v
	}
	return r.Header.Get("X-User-Id")
}

// parseBearer extracts and validates a bearer JWT from the request. The IM
// JWT uses the same shared secret as the account service tokens with a custom
// subject.
func (s *Server) parseBearer(r *http.Request) (*common.AccessClaims, error) {
	auth := r.Header.Get("Authorization")
	if len(auth) < 8 || auth[:7] != "Bearer " {
		return nil, common.ErrInvalidToken
	}
	return common.ParseAccessToken(s.cfg.JWTSecret, auth[7:])
}

// pathID parses a positive int64 path segment, writing a 400 and returning 0
// when it is missing or malformed.
func pathID(w http.ResponseWriter, raw string) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "路径参数非法")
		return 0
	}
	return n
}

// queryLimit returns the ?limit= value clamped to [1, max], defaulting to def.
func queryLimit(r *http.Request, def, max int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// peerPath extracts the opaque {peer} path segment, URL-decoded. uids coming
// from the account service are opaque strings, so no further validation.
func peerPath(w http.ResponseWriter, raw string) string {
	s, err := url.PathUnescape(raw)
	if err != nil || s == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "对端用户不合法")
		return ""
	}
	return s
}

// nowUnixMilli returns the server clock in milliseconds. Used for MsgTimeStamp.
func (s *Server) nowUnixMilli() int64 { return s.now().UnixMilli() }
