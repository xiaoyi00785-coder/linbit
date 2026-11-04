package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newLoggedGateway(t *testing.T, upstreamURL string) (http.Handler, *bytes.Buffer) {
	t.Helper()
	gw, err := NewGateway(Config{JWTSecret: "test-secret", AccountURL: upstreamURL, AssetURL: upstreamURL, MarketURL: upstreamURL, TrustedProxyCIDRs: "192.0.2.1/32"}) // httptest peer = nginx (trusted)
	if err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	gw.accessLog = newAccessLogger(buf)
	return gw.Handler(), buf
}

func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("bad log line %q: %v", buf.String(), err)
	}
	return m
}

func TestAccessLogFields(t *testing.T) {
	up := newUpstream()
	defer up.server.Close()
	h, buf := newLoggedGateway(t, up.server.URL)
	req := httptest.NewRequest("GET", "/market/ticker?symbol=BTCUSDT&token=abc123&apiKey=k&code=999", nil)
	req.Host = "api.cowa.click"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	req.Header.Set("X-Request-Id", "nginx-abc")
	req.Header.Set("Authorization", "Bearer supersecret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Header().Get("X-Request-Id") != "nginx-abc" {
		t.Fatalf("request id not echoed: %q", w.Header().Get("X-Request-Id"))
	}
	m := lastLine(t, buf)
	for k, want := range map[string]any{"method": "GET", "host": "api.cowa.click", "path": "/market/ticker", "status": float64(200), "ip": "203.0.113.9", "request_id": "nginx-abc"} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
	if _, ok := m["latency_ms"]; !ok {
		t.Error("missing latency_ms")
	}
	raw := buf.String()
	for _, secret := range []string{"abc123", "supersecret", "=k", "999"} {
		if strings.Contains(raw, secret) {
			t.Errorf("log leaked %q: %s", secret, raw)
		}
	}
	if !strings.Contains(m["query"].(string), "symbol=BTCUSDT") {
		t.Errorf("query lost non-secret params: %v", m["query"])
	}
}

func TestAccessLogCFIPAndGeneratedID(t *testing.T) {
	up := newUpstream()
	defer up.server.Close()
	h, buf := newLoggedGateway(t, up.server.URL)
	req := httptest.NewRequest("GET", "/v1/markets", nil)
	req.Header.Set("CF-Connecting-IP", "198.51.100.7")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Request-Id", "bad id with spaces")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	m := lastLine(t, buf)
	if m["ip"] != "198.51.100.7" {
		t.Errorf("ip = %v", m["ip"])
	}
	if id, _ := m["request_id"].(string); !strings.HasPrefix(id, "req_") {
		t.Errorf("request_id = %v, want generated", m["request_id"])
	}
	if m["status"] != float64(404) {
		t.Errorf("status = %v", m["status"])
	}
}

func TestAccessLogSkipsHealthAndDisabled(t *testing.T) {
	up := newUpstream()
	defer up.server.Close()
	h, buf := newLoggedGateway(t, up.server.URL)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("healthz logged: %s", buf.String())
	}
	t.Setenv("GATEWAY_ACCESS_LOG", "off")
	if accessLogEnabled() {
		t.Fatal("GATEWAY_ACCESS_LOG=off should disable")
	}
	t.Setenv("GATEWAY_ACCESS_LOG", "")
	if !accessLogEnabled() {
		t.Fatal("default should be on")
	}
}

func TestUnknownRoutes404BeforeAuth(t *testing.T) {
	up := newUpstream()
	defer up.server.Close()
	h, _ := newLoggedGateway(t, up.server.URL)
	for _, p := range []string{"/", "/v1/markets", "/markets", "/v1/account/me", "/v1/ws", "/ws/market", "/gw/market/ticker"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, w.Code)
		}
	}
	// Known protected prefixes still require auth.
	for _, p := range []string{"/account/me", "/assets/balances", "/spot/orders"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", p, w.Code)
		}
	}
	if up.hits != 0 {
		t.Fatalf("upstream hit %d times", up.hits)
	}
	// Public market + /ws still proxied.
	for _, p := range []string{"/market/ticker", "/ws"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", p, w.Code)
		}
	}
}

func TestRedactQuery(t *testing.T) {
	got := redactQuery("access_token=x&symbol=ETHUSDT&signature=y&limit=5")
	if strings.Contains(got, "=x") || strings.Contains(got, "=y") || !strings.Contains(got, "symbol=ETHUSDT") || !strings.Contains(got, "access_token=***") {
		t.Fatalf("redactQuery = %q", got)
	}
}

// WebSocket upgrades must still tunnel through the logging wrapper.
func TestAccessLogWebSocketUpgrade(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, brw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nhello")
		brw.Flush()
	}))
	defer up.Close()
	gw, err := NewGateway(Config{JWTSecret: "test-secret", MarketURL: up.URL})
	if err != nil {
		t.Fatal(err)
	}
	buf := &lockedBuf{}
	gw.accessLog = newAccessLogger(buf)
	srv := httptest.NewServer(gw.Handler())
	defer srv.Close()
	conn, err2 := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err2 != nil {
		t.Fatal(err2)
	}
	defer conn.Close()
	conn.Write([]byte("GET /ws HTTP/1.1\r\nHost: api.cowa.click\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, _ := io.ReadAll(conn)
	if !strings.HasPrefix(string(b), "HTTP/1.1 101") || !strings.Contains(string(b), "hello") {
		t.Fatalf("upgrade response = %q", b)
	}
	conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), `"status":101`) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), `"status":101`) {
		t.Fatalf("101 not logged: %s", buf.String())
	}
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
