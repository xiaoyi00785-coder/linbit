package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Hub tracks connected WebSocket clients and routes conversation events to the
// members of the conversation. Every member has one goroutine per connection
// plus a per-uid presence record so other sessions can read online state.
type Hub struct {
	srv *Server

	mu      sync.RWMutex
	clients map[string]map[*wsClient]struct{} // uid -> connections

	pingInterval time.Duration
	idleTimeout  time.Duration
}

const (
	hubPingInterval = 15 * time.Second
	hubIdleTimeout  = 90 * time.Second
	hubSendBuffer   = 128
)

// NewHub builds a Hub bound to the given server.
func NewHub(srv *Server) *Hub {
	return &Hub{
		srv:          srv,
		clients:      map[string]map[*wsClient]struct{}{},
		pingInterval: hubPingInterval,
		idleTimeout:  hubIdleTimeout,
	}
}

// wsClient is one connected socket.
type wsClient struct {
	hub    *Hub
	conn   *wsConn
	uid    string
	send   chan []byte
	closed chan struct{}
	once   sync.Once
}

// ServeHTTP upgrades the request and runs the client until it disconnects.
// The caller must have authenticated the request: if the gateway provided
// X-User-Id we use that; otherwise ?token= is parsed with the shared JWT.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		if tok := r.URL.Query().Get("token"); tok != "" {
			if claims, err := common.ParseAccessToken(h.srv.cfg.JWTSecret, tok); err == nil {
				uid = claims.Subject
			}
		}
	}
	if uid == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgradeWS(w, r)
	if err != nil {
		http.Error(w, "websocket upgrade failed", http.StatusBadRequest)
		return
	}
	c := &wsClient{
		hub:    h,
		conn:   conn,
		uid:    uid,
		send:   make(chan []byte, hubSendBuffer),
		closed: make(chan struct{}),
	}
	h.register(c)
	go c.writePump()
	c.readPump()
}

func (h *Hub) register(c *wsClient) {
	h.mu.Lock()
	m, ok := h.clients[c.uid]
	if !ok {
		m = map[*wsClient]struct{}{}
		h.clients[c.uid] = m
	}
	m[c] = struct{}{}
	h.mu.Unlock()
	go func() {
		_ = h.srv.store.touchPresence(context.Background(), c.uid, "ws")
	}()
}

func (h *Hub) unregister(c *wsClient) {
	h.mu.Lock()
	if m := h.clients[c.uid]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(h.clients, c.uid)
			go func(uid string) {
				_ = h.srv.store.clearPresence(context.Background(), uid)
			}(c.uid)
		}
	}
	h.mu.Unlock()
}

// pushUser delivers one JSON payload to every open socket of uid. Returns the
// number of sockets written to.
func (h *Hub) pushUser(uid string, payload []byte) int {
	h.mu.RLock()
	m := h.clients[uid]
	peers := make([]*wsClient, 0, len(m))
	for c := range m {
		peers = append(peers, c)
	}
	h.mu.RUnlock()
	for _, c := range peers {
		c.enqueue(payload)
	}
	return len(peers)
}

// broadcastConversation enqueues payload to every member of the conversation.
// Members are read from the DB on each call — chat conversations are small so
// the lookup is cheap, and this keeps the hub free of per-conversation state
// that would need invalidating on membership changes.
func (h *Hub) broadcastConversation(convID int64, event map[string]any) {
	event["conversationId"] = convID
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := h.srv.store.db.QueryContext(ctx, `SELECT uid FROM im_conversation_members WHERE conversation_id = $1`, convID)
	if err != nil {
		log.Printf("hub: conversation %d members: %v", convID, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		h.pushUser(uid, raw)
	}
}

// enqueue tries to push onto the client's send channel; a full channel kills
// the connection (the client is too slow to keep up).
func (c *wsClient) enqueue(b []byte) {
	select {
	case c.send <- b:
	default:
		c.close()
	}
}

func (c *wsClient) close() {
	c.once.Do(func() {
		close(c.closed)
		c.conn.close()
	})
}

func (c *wsClient) writePump() {
	ping := time.NewTicker(c.hub.pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.closed:
			return
		case b := <-c.send:
			if err := c.conn.writeText(b); err != nil {
				c.close()
				return
			}
		case <-ping.C:
			if err := c.conn.writeFrame(opPing, nil); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *wsClient) readPump() {
	defer func() {
		c.close()
		c.hub.unregister(c)
	}()
	_ = c.conn.conn.SetReadDeadline(time.Now().Add(c.hub.idleTimeout))
	// Greet the client with a hello frame so the Flutter SDK's onOpen fires.
	hello, _ := json.Marshal(map[string]any{"event": "hello", "userId": c.uid, "ts": time.Now().UnixMilli()})
	c.enqueue(hello)
	for {
		opcode, _, err := c.conn.readFrame()
		if err != nil {
			return
		}
		_ = c.conn.conn.SetReadDeadline(time.Now().Add(c.hub.idleTimeout))
		switch opcode {
		case opPing:
			_ = c.conn.writeFrame(opPong, nil)
		case opClose:
			return
		case opText, opBinary, opPong, opContinuation:
			// Clients don't send commands on the im socket — we only push. A
			// text frame is accepted as a cheap heartbeat and ignored.
		}
	}
}
