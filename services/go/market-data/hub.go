package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Snapshotter supplies the initial frame sent when a client subscribes, plus
// channel validation. The aggregator implements it.
type Snapshotter interface {
	HasSymbol(symbol string) bool
	ValidInterval(name string) bool
	DepthFrame(symbol string, limit int) (depthDTO, bool)
	TickerFrame(symbol string) (tickerDTO, bool)
	KlineFrame(symbol, interval string) (wsKline, bool)
}

const (
	defaultPingInterval = 15 * time.Second
	defaultIdleTimeout  = 60 * time.Second
	depthSnapshotLimit  = 50
	clientSendBuffer    = 256
)

// Hub tracks WebSocket clients and their channel subscriptions and fans out
// broadcasts. It implements Broadcaster for the aggregator.
type Hub struct {
	snap         Snapshotter
	pingInterval time.Duration
	idleTimeout  time.Duration
	now          func() time.Time

	// chatValidate validates a "chat:{room}" channel (set by chatRoutes).
	chatValidate func(room string) (code, msg string)

	mu       sync.RWMutex
	channels map[string]map[*wsClient]struct{}

	// last frame per relayed channel (futures "mark:{SYMBOL}"), replayed on subscribe.
	lmu  sync.RWMutex
	last map[string]json.RawMessage
}

// markChannelRe matches the futures mark-price channel pushed by the futures service.
var markChannelRe = regexp.MustCompile(`^mark:[A-Z0-9]{2,30}$`)

// Relay broadcasts a frame produced by another service and remembers it as the
// channel snapshot for new subscribers.
func (h *Hub) Relay(channel string, data json.RawMessage) {
	h.lmu.Lock()
	if h.last == nil {
		h.last = map[string]json.RawMessage{}
	}
	h.last[channel] = data
	h.lmu.Unlock()
	h.Broadcast(channel, data)
}

// NewHub builds a hub over the given snapshot source.
func NewHub(snap Snapshotter) *Hub {
	return &Hub{
		snap:         snap,
		pingInterval: defaultPingInterval,
		idleTimeout:  defaultIdleTimeout,
		now:          time.Now,
		channels:     map[string]map[*wsClient]struct{}{},
	}
}

// outFrame is a queued outbound WebSocket frame.
type outFrame struct {
	opcode byte
	data   []byte
}

// wsClient is a single connected client.
type wsClient struct {
	hub  *Hub
	conn *wsConn
	send chan outFrame
	subs map[string]struct{}
	uid  string // set by the gateway (X-User-Id) when the handshake carried a valid token

	closeOnce sync.Once
	closed    chan struct{}
}

// ---- wire messages ----

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
		uid:    r.Header.Get("X-User-Id"),
		closed: make(chan struct{}),
	}
	go c.writePump()
	c.readPump()
}

// userChannels are private channels: the subscription is keyed by the
// connection's user so a frame reaches only that user's sockets.
var userChannels = map[string]bool{"event:orders": true}

func userKey(channel, uid string) string { return channel + "#" + uid }

// subKey maps a client-visible channel to the hub key.
func (c *wsClient) subKey(channel string) string {
	if userChannels[channel] {
		return userKey(channel, c.uid)
	}
	return channel
}

// PushUser delivers one frame on a private channel to every socket of uid.
func (h *Hub) PushUser(uid, channel string, data any) int {
	b, err := json.Marshal(wsData{Channel: channel, TS: h.now().UnixMilli(), Data: data})
	if err != nil {
		return 0
	}
	h.mu.RLock()
	subs := h.channels[userKey(channel, uid)]
	clients := make([]*wsClient, 0, len(subs))
	for c := range subs {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		c.enqueue(outFrame{opcode: opText, data: b})
	}
	return len(clients)
}

// Broadcast implements Broadcaster: it serializes one data frame and delivers it
// to every client subscribed to the channel.
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

// channelCount returns the number of subscribers on a channel (test helper).
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
		// Slow consumer: drop it rather than block matching/broadcast.
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
			// App-level heartbeat per docs/api/websocket.md.
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
			// Deadline already refreshed by the successful read.
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
		// no-op
	case "subscribe":
		for _, ch := range req.Args {
			c.doSubscribe(req.ID, ch)
		}
	case "unsubscribe":
		for _, ch := range req.Args {
			c.hub.unsubscribe(c, c.subKey(ch))
			delete(c.subs, c.subKey(ch))
			c.sendJSON(wsReceipt{ID: req.ID, Event: "unsubscribed", Channel: ch})
		}
	case "auth":
		c.sendJSON(wsError{ID: req.ID, Event: "error", Code: "SUB_INVALID",
			Message: "本服务仅提供公开行情频道，用户频道请连接账户/订单服务"})
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
	c.hub.subscribe(c, c.subKey(channel))
	c.subs[c.subKey(channel)] = struct{}{}
	c.sendJSON(wsReceipt{ID: id, Event: "subscribed", Channel: channel})
	c.sendInitial(channel)
}

// validateChannel checks a channel string, returning an error code+message when
// it is not a supported public market channel.
func (c *wsClient) validateChannel(channel string) (code, msg string) {
	parts := strings.Split(channel, ":")
	switch parts[0] {
	case "ticker", "depth", "trades":
		if len(parts) != 2 || !c.hub.snap.HasSymbol(parts[1]) {
			return "SUB_INVALID", "无效的频道或交易对: " + channel
		}
	case "kline":
		if len(parts) != 3 || !c.hub.snap.HasSymbol(parts[1]) || !c.hub.snap.ValidInterval(parts[2]) {
			return "SUB_INVALID", "无效的 K 线频道: " + channel
		}
	case "mark":
		if !markChannelRe.MatchString(channel) {
			return "SUB_INVALID", "无效的标记价格频道: " + channel
		}
	case "chat":
		if len(parts) != 2 || c.hub.chatValidate == nil {
			return "SUB_INVALID", "无效的聊天室频道: " + channel
		}
		return c.hub.chatValidate(parts[1])
	case "event":
		if channel != "event:orders" {
			return "SUB_INVALID", "未知频道: " + channel
		}
		if c.uid == "" {
			return "AUTH_REQUIRED", "私有频道需登录：连接 /ws 时携带 Authorization: Bearer 或 ?token="
		}
	case "orders", "balances":
		return "SUB_INVALID", "用户频道由账户/订单服务提供"
	default:
		return "SUB_INVALID", "未知频道: " + channel
	}
	return "", ""
}

// sendInitial pushes the current snapshot for a freshly subscribed channel.
func (c *wsClient) sendInitial(channel string) {
	parts := strings.Split(channel, ":")
	switch parts[0] {
	case "ticker":
		if d, ok := c.hub.snap.TickerFrame(parts[1]); ok {
			c.sendData(channel, d)
		}
	case "depth":
		if d, ok := c.hub.snap.DepthFrame(parts[1], depthSnapshotLimit); ok {
			c.sendData(channel, d)
		}
	case "kline":
		if d, ok := c.hub.snap.KlineFrame(parts[1], parts[2]); ok {
			c.sendData(channel, d)
		}
	case "mark":
		c.hub.lmu.RLock()
		d, ok := c.hub.last[channel]
		c.hub.lmu.RUnlock()
		if ok {
			c.sendData(channel, d)
		}
	}
}

func (c *wsClient) sendData(channel string, data any) {
	b, err := json.Marshal(wsData{Channel: channel, TS: c.hub.now().UnixMilli(), Data: data})
	if err != nil {
		return
	}
	c.enqueue(outFrame{opcode: opText, data: b})
}

func (c *wsClient) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.enqueue(outFrame{opcode: opText, data: b})
}
