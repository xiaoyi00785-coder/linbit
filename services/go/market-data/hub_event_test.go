package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHubPrivateEventChannel(t *testing.T) {
	h := NewHub(fakeSnap{})
	anon := newTestClient(h)
	if code, _ := anon.validateChannel("event:orders"); code != "AUTH_REQUIRED" {
		t.Fatalf("anon event:orders = %q", code)
	}
	a, b := newTestClient(h), newTestClient(h)
	a.uid, b.uid = "80000001", "80000002"
	if code, _ := a.validateChannel("event:orders"); code != "" {
		t.Fatalf("authed rejected: %s", code)
	}
	if code, _ := a.validateChannel("event:other"); code == "" {
		t.Fatal("event:other accepted")
	}
	a.doSubscribe("r1", "event:orders")
	b.doSubscribe("r2", "event:orders")
	nextFrame(t, a)
	nextFrame(t, b)
	if n := h.PushUser("80000001", "event:orders", map[string]string{"id": "1"}); n != 1 {
		t.Fatalf("delivered %d", n)
	}
	var f wsData
	if err := json.Unmarshal(nextFrame(t, a), &f); err != nil || f.Channel != "event:orders" {
		t.Fatalf("frame %+v %v", f, err)
	}
	select {
	case <-b.send:
		t.Fatal("other user received a private frame")
	case <-time.After(50 * time.Millisecond):
	}
	h.Broadcast("event:orders", "x") // the shared key has no subscribers
	select {
	case <-a.send:
		t.Fatal("broadcast leaked into private channel")
	case <-time.After(50 * time.Millisecond):
	}
}
