package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupportProxy(t *testing.T) {
	var got []string
	var body map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(r.URL.Path, "/attachments/") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer up.Close()
	db, _ := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1")
	defer db.Close()
	s := NewServer(Config{AccountURL: up.URL}, db)
	a := &actor{ID: 42, Username: "cs1", Roles: []string{roleSupport}}
	do := func(h http.HandlerFunc, method, path, id, payload string) *httptest.ResponseRecorder {
		var r *http.Request
		if payload != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(payload))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		if id != "" {
			r.SetPathValue("id", id)
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, a))
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}

	do(s.handleSupportTickets, "GET", "/admin/v1/support/tickets?status=open&assigneeId=me&uid=80000001&bogus=1", "", "")
	if got[0] != "GET /internal/admin/support/tickets?assigneeId=42&status=open&uid=80000001" {
		t.Fatalf("list path %q", got[0])
	}
	if w := do(s.handleSupportReply, "POST", "/x", "9", `{"content":" "}`); w.Code != 400 {
		t.Fatalf("empty reply = %d", w.Code)
	}
	if w := do(s.handleSupportReply, "POST", "/x", "9", `{"content":"hi","status":"closed"}`); w.Code != 200 {
		t.Fatalf("reply = %d", w.Code)
	}
	if got[1] != "POST /internal/admin/support/tickets/9/messages" || body["adminId"] != float64(42) || body["adminName"] != "cs1" || body["status"] != "closed" {
		t.Fatalf("reply upstream %v %v", got, body)
	}
	if w := do(s.handleSupportPatch, "PATCH", "/x", "9", `{}`); w.Code != 400 {
		t.Fatalf("empty patch = %d", w.Code)
	}
	if w := do(s.handleSupportPatch, "PATCH", "/x", "9", `{"status":"open","assigneeId":0}`); w.Code != 200 || body["status"] != "open" || body["assigneeId"] != float64(0) {
		t.Fatalf("patch = %d %v", w.Code, body)
	}
	w := do(s.handleSupportAttachment, "GET", "/x", "a.png", "")
	if w.Header().Get("Content-Type") != "image/png" || w.Body.String() != "PNG" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("attachment %v %q", w.Header(), w.Body.String())
	}
}
