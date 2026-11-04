package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeUserText(t *testing.T) {
	cases := map[string]string{
		"see https://evil.io/x now":    "see https[:]//evil.io/x now",
		"HTTP://a.b":                   "HTTP[:]//a.b",
		"go www.phish.com":             "go www[.]phish.com",
		"javascript:alert(1)":          "javascript[:]alert(1)",
		"a\u202Eb\x07c\r\nd":           "abc\nd",
		"  x\n\n\n\n\n\ny  ":           "x\n\n\ny",
		"ftp://h/f and data:text/html": "ftp[:]//h/f and data[:]text/html",
	}
	for in, want := range cases {
		if got := sanitizeUserText(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func uploadTicketImage(t *testing.T, h http.Handler, token string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartFile(t, "file", "s.png", content)
	r := httptest.NewRequest("POST", "/support/attachments", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func internalReq(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, strings.NewReader(string(b)))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Internal-Token", "test-internal")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSupportTicketLifecycle(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	alice := registerWithCode(t, srv, uniqueEmail(), "", "")
	bob := registerWithCode(t, srv, uniqueEmail(), "", "")

	// Upload: non-image rejected, PNG accepted, served only to owner.
	if w := uploadTicketImage(t, h, alice.AccessToken, []byte("<svg onload=x>")); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("svg upload = %d", w.Code)
	}
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("pixels")...)
	w := uploadTicketImage(t, h, alice.AccessToken, png)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", w.Code, w.Body.String())
	}
	var att ticketAttachment
	_ = json.Unmarshal(w.Body.Bytes(), &att)
	if w := doJSON(t, h, "GET", "/support/attachments/"+att.ID, bob.AccessToken, nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob reads alice attachment = %d", w.Code)
	}
	if w := doJSON(t, h, "GET", "/support/attachments/"+att.ID, alice.AccessToken, nil); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("owner read = %d %s", w.Code, w.Header().Get("Content-Type"))
	}

	// Validation.
	if w := doJSON(t, h, "POST", "/support/tickets", alice.AccessToken, map[string]any{"category": "nope", "subject": "s", "content": "c"}); w.Code != 400 {
		t.Fatalf("bad category = %d", w.Code)
	}
	// Bob cannot attach Alice's upload.
	if w := doJSON(t, h, "POST", "/support/tickets", bob.AccessToken, map[string]any{"category": "trade", "subject": "s", "content": "c", "attachments": []string{att.ID}}); w.Code != 400 {
		t.Fatalf("foreign attachment = %d %s", w.Code, w.Body.String())
	}

	w = doJSON(t, h, "POST", "/support/tickets", alice.AccessToken, map[string]any{
		"category": "deposit_withdraw", "subject": "充值未到账", "content": "tx at https://scan.io/tx/1", "attachments": []string{att.ID}})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var tk ticketDTO
	_ = json.Unmarshal(w.Body.Bytes(), &tk)
	if tk.Status != "open" || len(tk.Messages) != 1 || tk.Messages[0].Body != "tx at https[:]//scan.io/tx/1" || len(tk.Messages[0].Attachments) != 1 {
		t.Fatalf("created ticket = %+v", tk)
	}
	// Attachment cannot be reused.
	if w := doJSON(t, h, "POST", "/support/tickets/"+tk.ID+"/messages", alice.AccessToken, map[string]any{"content": "again", "attachments": []string{att.ID}}); w.Code != 400 {
		t.Fatalf("reuse attachment = %d", w.Code)
	}
	// Other users see 404.
	if w := doJSON(t, h, "GET", "/support/tickets/"+tk.ID, bob.AccessToken, nil); w.Code != 404 {
		t.Fatalf("bob reads ticket = %d", w.Code)
	}

	// Internal API requires the token; admin lists, assigns, replies.
	if w := doJSON(t, h, "GET", "/internal/admin/support/tickets", "", nil); w.Code != 401 {
		t.Fatalf("internal no token = %d", w.Code)
	}
	var acct accountResponse
	_ = json.Unmarshal(doJSON(t, h, "GET", "/account", alice.AccessToken, nil).Body.Bytes(), &acct)
	w = internalReq(t, h, "GET", "/internal/admin/support/tickets?status=open&category=deposit_withdraw&uid="+acct.UID, nil)
	var list struct {
		Items []ticketDTO `json:"items"`
		Total int         `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || list.Total != 1 || list.Items[0].ID != tk.ID || list.Items[0].UID != acct.UID {
		t.Fatalf("internal list = %d %s", w.Code, w.Body.String())
	}
	if w := internalReq(t, h, "PATCH", "/internal/admin/support/tickets/"+tk.ID, map[string]any{"assigneeId": 7, "assigneeName": "cs1"}); w.Code != 200 {
		t.Fatalf("assign = %d %s", w.Code, w.Body.String())
	}
	if w := internalReq(t, h, "POST", "/internal/admin/support/tickets/"+tk.ID+"/messages", map[string]any{"content": "已处理", "adminId": 7, "adminName": "cs1"}); w.Code != 200 {
		t.Fatalf("admin reply = %d %s", w.Code, w.Body.String())
	}

	// User sees unread reply from "客服" (no admin name leaked), then it's read.
	w = doJSON(t, h, "GET", "/support/tickets", alice.AccessToken, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 || !list.Items[0].Unread || list.Items[0].Status != "replied" || list.Items[0].AssigneeName != "" {
		t.Fatalf("user list = %s", w.Body.String())
	}
	w = doJSON(t, h, "GET", "/support/tickets/"+tk.ID, alice.AccessToken, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &tk)
	if len(tk.Messages) != 2 || tk.Messages[1].AuthorName != "客服" || tk.Messages[0].Attachments[0].URL == "" {
		t.Fatalf("thread = %s", w.Body.String())
	}
	_ = json.Unmarshal(doJSON(t, h, "GET", "/support/tickets", alice.AccessToken, nil).Body.Bytes(), &list)
	if list.Items[0].Unread {
		t.Fatal("still unread after viewing")
	}

	// User replies -> open again; close -> further replies rejected.
	w = doJSON(t, h, "POST", "/support/tickets/"+tk.ID+"/messages", alice.AccessToken, map[string]any{"content": "谢谢"})
	_ = json.Unmarshal(w.Body.Bytes(), &tk)
	if w.Code != 200 || tk.Status != "open" {
		t.Fatalf("user reply = %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "POST", "/support/tickets/"+tk.ID+"/close", alice.AccessToken, nil); w.Code != 200 {
		t.Fatalf("close = %d", w.Code)
	}
	if w := doJSON(t, h, "POST", "/support/tickets/"+tk.ID+"/messages", alice.AccessToken, map[string]any{"content": "x"}); w.Code != http.StatusConflict {
		t.Fatalf("reply closed = %d", w.Code)
	}
}

func TestSupportTicketRateLimit(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.TicketsOpenMax = 2
	h := srv.Routes()
	u := registerWithCode(t, srv, uniqueEmail(), "", "")
	for i := 0; i < 2; i++ {
		if w := doJSON(t, h, "POST", "/support/tickets", u.AccessToken, map[string]any{"category": "other", "subject": "s", "content": "c"}); w.Code != 201 {
			t.Fatalf("create %d = %d", i, w.Code)
		}
	}
	w := doJSON(t, h, "POST", "/support/tickets", u.AccessToken, map[string]any{"category": "other", "subject": "s", "content": "c"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third open ticket = %d", w.Code)
	}
}
