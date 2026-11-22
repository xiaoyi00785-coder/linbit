package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func reqFrom(peer string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("POST", "/cs/guest/conversations", nil)
	r.RemoteAddr = peer
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIPUntrustedPeerIgnoresHeaders(t *testing.T) {
	r := reqFrom("203.0.113.9:5555", map[string]string{"X-Real-IP": "1.2.3.4", "X-Forwarded-For": "1.2.3.4, 5.6.7.8"})
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("spoofed headers from public peer honoured: %q", got)
	}
}

func TestClientIPTrustedPeerUsesRealIP(t *testing.T) {
	// gateway container on the docker bridge; gateway appends its peer to XFF.
	r := reqFrom("172.18.0.27:41000", map[string]string{"X-Real-IP": "198.51.100.7", "X-Forwarded-For": "198.51.100.7, 172.18.0.1"})
	if got := clientIP(r); got != "198.51.100.7" {
		t.Fatalf("got %q", got)
	}
	r = reqFrom("[::1]:41000", map[string]string{"X-Forwarded-For": "2001:db8::5, 10.0.0.1"})
	if got := clientIP(r); got != "2001:db8::5" {
		t.Fatalf("xff fallback got %q", got)
	}
}

func TestClientIPTrustedPeerGarbageFallsBackToPeer(t *testing.T) {
	r := reqFrom("127.0.0.1:9", map[string]string{"X-Real-IP": "not-an-ip", "X-Forwarded-For": "evil<script>"})
	if got := clientIP(r); got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCIDRListBareIPs(t *testing.T) {
	n := parseCIDRList("172.18.0.1, ::1, bogus, 10.0.0.0/8")
	if len(n) != 3 {
		t.Fatalf("want 3 nets, got %d", len(n))
	}
}

func TestIPAllowCapAcrossGuestKeys(t *testing.T) {
	c := &CS{guestHits: map[string][]time.Time{}, srv: &Server{cfg: Config{GuestMinIntervalMs: 3000, GuestMaxPerHour: 60, GuestIPMaxPerMin: 3}}}
	for i, gk := range []string{"guestkey-0001", "guestkey-0002", "guestkey-0003"} {
		w := httptest.NewRecorder()
		if !c.guestLimit(w, reqFrom("172.18.0.27:1", map[string]string{"X-Real-IP": "198.51.100.20"}), gk, "x") {
			t.Fatalf("send %d with fresh guestKey should pass", i)
		}
	}
	w := httptest.NewRecorder()
	if c.guestLimit(w, reqFrom("172.18.0.27:1", map[string]string{"X-Real-IP": "198.51.100.20"}), "guestkey-0004", "x") || w.Code != 429 {
		t.Fatalf("4th key from same IP should be 429, code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	if !c.guestLimit(w, reqFrom("172.18.0.27:1", map[string]string{"X-Real-IP": "198.51.100.21"}), "guestkey-0004", "x") {
		t.Fatal("other IP must not be affected")
	}
}
