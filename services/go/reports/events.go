package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/linbit/platform/services/go/common"
)

// eventHub is a per-user fan-out of activity-stream frames. Each user owns one
// or more sockets; publish() writes to every socket's bounded send channel.
// A slow or dead socket is dropped so it never blocks the publisher.
type eventHub struct {
	mu    sync.RWMutex
	users map[string]map[*eventClient]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{users: map[string]map[*eventClient]struct{}{}}
}

type eventClient struct {
	uid   string
	send  chan []byte
	close chan struct{}
	once  sync.Once
}

func (c *eventClient) terminate() {
	c.once.Do(func() { close(c.close) })
}

func (h *eventHub) register(c *eventClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.users[c.uid]
	if !ok {
		set = map[*eventClient]struct{}{}
		h.users[c.uid] = set
	}
	set[c] = struct{}{}
}

func (h *eventHub) unregister(c *eventClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.users[c.uid]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.users, c.uid)
		}
	}
}

// publish delivers frame to every live socket for uid. Each client has a
// bounded send buffer; one that is full is closed and dropped on this pass.
func (h *eventHub) publish(uid string, frame []byte) {
	h.mu.RLock()
	set := h.users[uid]
	clients := make([]*eventClient, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- frame:
		default:
			c.terminate()
		}
	}
}

// GET /account/events?token=<access>
// The gateway will NOT forward tokens on WebSocket upgrades in every setup, so
// the handler validates the token itself. Browsers cannot set Authorization on
// a native WS handshake, hence the ?token= query.
func (s *Server) handleEventsWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	tok = strings.TrimSpace(tok)
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		if tok == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "缺少访问令牌")
			return
		}
		claims, err := common.ParseAccessToken(s.cfg.JWTSecret, tok)
		if err != nil {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "访问令牌无效")
			return
		}
		uid = claims.Subject
	}
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "身份校验失败")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Dev convenience: a browser from any origin may connect. The gateway
		// is the production-facing edge and enforces CORS at that layer.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := &eventClient{uid: uid, send: make(chan []byte, 64), close: make(chan struct{})}
	s.hub.register(client)
	defer s.hub.unregister(client)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Greet with a hello frame so the client knows the subscription is live.
	hello, _ := json.Marshal(map[string]any{"type": "hello", "uid": uid, "time": time.Now().UnixMilli()})
	_ = conn.Write(r.Context(), websocket.MessageText, hello)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Reader: keep the connection alive by draining incoming frames and
	// terminating on error / close.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				client.terminate()
				cancel()
				return
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-client.close:
			return
		case frame := <-client.send:
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			if err := conn.Write(wctx, websocket.MessageText, frame); err != nil {
				wcancel()
				return
			}
			wcancel()
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
			if err := conn.Ping(pctx); err != nil {
				pcancel()
				return
			}
			pcancel()
		}
	}
}

// POST /internal/events/push — producers outside this service push one frame
// to a user's activity stream. Body: {"uid":"...", "event":{...}}.
func (s *Server) handleInternalEventPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID   string          `json:"uid"`
		Event json.RawMessage `json:"event"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.UID == "" || len(req.Event) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求无效")
		return
	}
	s.hub.publish(req.UID, []byte(req.Event))
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
