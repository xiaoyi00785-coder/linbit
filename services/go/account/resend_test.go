package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendSender(t *testing.T) {
	var got map[string]interface{}
	var auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"id":"x"}`))
	}))
	defer ts.Close()
	rec := NewCodeRecorder()
	s := newResendSender(ts.URL, "re_test", "Linbit <no-reply@example.com>", rec)
	if err := s.SendEmailCode(context.Background(), "a@example.com", "123456", purposeRegister); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer re_test" || got["from"] != "Linbit <no-reply@example.com>" || !strings.Contains(got["text"].(string), "123456") {
		t.Fatalf("bad request: %s %v", auth, got)
	}
	if rec.Last("a@example.com", purposeRegister) != "123456" {
		t.Fatal("not recorded")
	}
	// nil recorder (production) must not panic; upstream error surfaces.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(422) }))
	defer bad.Close()
	if err := newResendSender(bad.URL, "k", "f", nil).SendEmail(context.Background(), "a@b.c", "s", "t"); err == nil {
		t.Fatal("expected error")
	}
}
