package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultPingInterval = 15 * time.Second
	defaultIdleTimeout  = 60 * time.Second
	clientSendBuffer    = 256
)

// Hub tracks WebSocket clients and their channel subscriptions and fans out
// broadcasts. chat-svc only serves "chat:{room}" and "cs:{pubToken}" channels;
// both are possession/validation-gated (no market snapshots).
type Hub struct {
	pingInterval time.Duration
	idleTimeout  time.Duration
	now          func() time.Time

	// Validators are set by the chat / cs route wiring. Each returns ("","")
	// when the channel may be subscribed, else an error code+message.
	chatValidate func(room string) (code, msg string)
	csValidate   func(token string) (code, msg string)

	mu       sync.RWMutex
	channels map[string]map[*wsClient]struct{}
}

func NewHub() *Hub {
	return &Hub{
		pingInterval: defaultPingInterval,
		idleTimeout:  defaultIdleTimeout,
		now:          time.Now,
		channels:     map[string]map[*wsClient]struct{}{},
	}
}

type outFrame struct {
	opcode byte
	data   []byte
}

type wsClient struct {
	hub  *Hub
	conn *wsConn
	send chan outFrame
	subs map[string]struct{}

	closeOnce sync.Once
	closed    chan struct{}
}

type wsRequest struct {
	ID   string   `json:"id"`
	Op   string   `json:"op"`
	Args []string `json:"args"`
}

type wsReceipt struct {
	ID      string `json:"id,omitempty"`
	Event   string `json:"event"`
	Channel string `json:"channel,omitempty"`
}

type wsError struct {
	ID      string `json:"id,omitempty"`
	Event   string `json:"event"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wsData struct {
	Channel string `json:"channel"`
	TS      int64  `json:"ts"`
	Data    any    `json:"data"`
}

// ServeHTTP upgrades the request and runs the client until it disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgradeWS(w, r)
	if err != nil {
		http.Error(w, "websocket upgrade failed", http.StatusBadRequest)
		return
	}
	c := &wsClient{
		hub:    h,
		conn:   conn,
		send:   make(chan outFrame, clientSendBuffer),
		subs:   map[string]struct{}{},
		closed: make(chan struct{}),
	}
	go c.writePump()
	c.readPump()
}

// Broadcast serializes one data frame and delivers it to every subscriber.
func (h *Hub) Broadcast(channel string, data any) {
	frame := wsData{Channel: channel, TS: h.now().UnixMilli(), Data: data}
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	h.mu.RLock()
	subs := h.channels[channel]
	clients := make([]*wsClient, 0, len(subs))
	for c := range subs {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		c.enqueue(outFrame{opcode: opText, data: b})
	}
}

func (h *Hub) subscribe(c *wsClient, channel string) {
	h.mu.Lock()
	set := h.channels[channel]
	if set == nil {
		set = map[*wsClient]struct{}{}
		h.channels[channel] = set
	}
	set[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unsubscribe(c *wsClient, channel string) {
	h.mu.Lock()
	if set := h.channels[channel]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.channels, channel)
		}
	}
	h.mu.Unlock()
}

func (h *Hub) removeClient(c *wsClient) {
	h.mu.Lock()
	for ch := range c.subs {
		if set := h.channels[ch]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.channels, ch)
			}
		}
	}
	h.mu.Unlock()
}

func (h *Hub) channelCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[channel])
}

// ---- client pumps ----

func (c *wsClient) enqueue(f outFrame) {
	select {
	case c.send <- f:
	case <-c.closed:
	default:
		c.close()
	}
}

func (c *wsClient) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.conn.Close()
		c.hub.removeClient(c)
	})
}

func (c *wsClient) writePump() {
	ping := time.NewTicker(c.hub.pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.closed:
			return
		case f := <-c.send:
			if err := c.conn.writeFrame(f.opcode, f.data); err != nil {
				c.close()
				return
			}
		case <-ping.C:
			c.enqueue(outFrame{opcode: opText, data: []byte(`{"event":"ping"}`)})
		}
	}
}

func (c *wsClient) readPump() {
	defer c.close()
	for {
		_ = c.conn.setReadDeadline(c.hub.now().Add(c.hub.idleTimeout))
		opcode, payload, err := c.conn.readFrame()
		if err != nil {
			return
		}
		switch opcode {
		case opClose:
			_ = c.conn.writeClose()
			return
		case opPing:
			c.enqueue(outFrame{opcode: opPong, data: payload})
		case opPong:
		case opText:
			c.handle(payload)
		}
	}
}

func (c *wsClient) handle(payload []byte) {
	var req wsRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		c.sendJSON(wsError{Event: "error", Code: "VALIDATION_ERROR", Message: "消息格式错误"})
		return
	}
	switch strings.ToLower(req.Op) {
	case "ping":
		c.sendJSON(map[string]any{"event": "pong", "ts": c.hub.now().UnixMilli()})
	case "pong":
	case "subscribe":
		for _, ch := range req.Args {
			c.doSubscribe(req.ID, ch)
		}
	case "unsubscribe":
		for _, ch := range req.Args {
			c.hub.unsubscribe(c, ch)
			delete(c.subs, ch)
			c.sendJSON(wsReceipt{ID: req.ID, Event: "unsubscribed", Channel: ch})
		}
	default:
		c.sendJSON(wsError{ID: req.ID, Event: "error", Code: "VALIDATION_ERROR", Message: "未知的 op"})
	}
}

func (c *wsClient) doSubscribe(id, channel string) {
	code, msg := c.validateChannel(channel)
	if code != "" {
		c.sendJSON(wsError{ID: id, Event: "error", Code: code, Message: msg})
		return
	}
	c.hub.subscribe(c, channel)
	c.subs[channel] = struct{}{}
	c.sendJSON(wsReceipt{ID: id, Event: "subscribed", Channel: channel})
	// History is fetched over REST; no snapshot frame on subscribe.
}

func (c *wsClient) validateChannel(channel string) (code, msg string) {
	parts := strings.SplitN(channel, ":", 2)
	if len(parts) != 2 {
		return "SUB_INVALID", "未知频道: " + channel
	}
	switch parts[0] {
	case "chat":
		if c.hub.chatValidate == nil {
			return "SUB_INVALID", "无效的聊天室频道: " + channel
		}
		return c.hub.chatValidate(parts[1])
	case "cs":
		if c.hub.csValidate == nil {
			return "SUB_INVALID", "无效的客服频道: " + channel
		}
		return c.hub.csValidate(parts[1])
	default:
		return "SUB_INVALID", "未知频道: " + channel
	}
}

func (c *wsClient) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.enqueue(outFrame{opcode: opText, data: b})
}
