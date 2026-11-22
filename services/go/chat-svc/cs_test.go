package main

import (
	"testing"
	"time"
)

func TestPublicSender(t *testing.T) {
	cases := map[string]string{"staff": "客服", "system": "系统", "user": ""}
	for kind, want := range cases {
		if got := publicSender(kind); got != want {
			t.Errorf("publicSender(%q)=%q want %q", kind, got, want)
		}
	}
}

func TestValidImageURL(t *testing.T) {
	if !validImageURL("/cs/uploads/abc.png") {
		t.Error("own-upload URL should be valid")
	}
	for _, u := range []string{"https://evil.example/x.png", "/other/x.png", "", "/cs/uploadsx/a"} {
		if validImageURL(u) {
			t.Errorf("url %q should be rejected", u)
		}
	}
}

func TestGuestKeyOK(t *testing.T) {
	if guestKeyOK("short") {
		t.Error("too-short key accepted")
	}
	if !guestKeyOK("abcdefgh12345") {
		t.Error("valid key rejected")
	}
	long := make([]byte, 81)
	for i := range long {
		long[i] = 'a'
	}
	if guestKeyOK(string(long)) {
		t.Error("too-long key accepted")
	}
}

func TestGuestAllowRateAndCap(t *testing.T) {
	c := &CS{guestHits: map[string][]time.Time{}, srv: &Server{cfg: Config{GuestMinIntervalMs: 3000, GuestMaxPerHour: 3}}}
	key := "1.2.3.4|guestabc123"
	if ok, _ := c.guestAllow(key); !ok {
		t.Fatal("first send should pass")
	}
	// Immediate second send violates the min interval.
	if ok, retry := c.guestAllow(key); ok || retry <= 0 {
		t.Fatalf("second send should be rate-limited, ok=%v retry=%d", ok, retry)
	}
	// Simulate time passing by rewriting the stored timestamps into the past.
	c.gmu.Lock()
	c.guestHits[key] = []time.Time{time.Now().Add(-10 * time.Second)}
	c.gmu.Unlock()
	if ok, _ := c.guestAllow(key); !ok {
		t.Fatal("send after interval should pass")
	}
	// Push to the hourly cap.
	c.gmu.Lock()
	c.guestHits[key] = []time.Time{time.Now().Add(-30 * time.Second), time.Now().Add(-20 * time.Second), time.Now().Add(-10 * time.Second)}
	c.gmu.Unlock()
	if ok, _ := c.guestAllow(key); ok {
		t.Fatal("send over hourly cap should be denied")
	}
}

func TestNewPubTokenUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tok := newPubToken()
		if len(tok) != 32 {
			t.Fatalf("token len=%d want 32", len(tok))
		}
		if seen[tok] {
			t.Fatal("duplicate token")
		}
		seen[tok] = true
	}
}
