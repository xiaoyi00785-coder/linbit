package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestSupportReplyNotificationLocator(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	alice := registerWithCode(t, srv, uniqueEmail(), "", "")
	bob := registerWithCode(t, srv, uniqueEmail(), "", "")

	w := doJSON(t, h, "POST", "/support/tickets", alice.AccessToken, map[string]any{
		"category": "other", "subject": "提现延迟", "content": "还没到",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var tk ticketDTO
	if err := json.Unmarshal(w.Body.Bytes(), &tk); err != nil || tk.ID == "" {
		t.Fatalf("ticket: %v %s", err, w.Body.String())
	}

	var acct accountResponse
	if err := json.Unmarshal(doJSON(t, h, "GET", "/account", alice.AccessToken, nil).Body.Bytes(), &acct); err != nil || acct.UID == "" {
		t.Fatal(err)
	}
	owner, err := srv.store.UserByUID(t.Context(), acct.UID)
	if err != nil {
		t.Fatal(err)
	}
	legacyBody := "请查看工单「" + tk.ID + "」https://evil.example/t/" + tk.ID
	legacyID, err := srv.store.InsertNotification(t.Context(), owner.ID, "system", "旧系统消息", legacyBody, "none")
	if err != nil {
		t.Fatal(err)
	}
	tradeID, err := srv.store.InsertNotification(t.Context(), owner.ID, "trade", "成交", "工单 "+tk.ID, "none")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.InsertNotificationRef(t.Context(), owner.ID, "system", "bad", "x", "none", "https://evil.example", tk.ID); err == nil {
		t.Fatal("external resource type was stored")
	}

	w = internalReq(t, h, "POST", "/internal/admin/support/tickets/"+tk.ID+"/messages", map[string]any{
		"content": "已核对", "adminId": 9, "adminName": "cs",
	})
	if w.Code != 200 {
		t.Fatalf("admin reply = %d %s", w.Code, w.Body.String())
	}
	w = internalReq(t, h, "POST", "/internal/notifications", map[string]any{
		"uid": acct.UID, "kind": "security", "title": "登录提醒", "body": "工单 " + tk.ID + " 无关",
	})
	if w.Code != 200 {
		t.Fatalf("plain notify = %d %s", w.Code, w.Body.String())
	}

	w = doJSON(t, h, "GET", "/notifications?pageSize=50", alice.AccessToken, nil)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	raw := w.Body.String()
	var page struct {
		Items []struct {
			ID    string `json:"id"`
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Body  string `json:"body"`
			Read  bool   `json:"read"`
			Link  *struct {
				Type     string `json:"type"`
				TicketID string `json:"ticketId"`
			} `json:"link"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	var replyID string
	for _, it := range page.Items {
		switch it.ID {
		case strconv.FormatInt(legacyID, 10), strconv.FormatInt(tradeID, 10):
			if it.Link != nil {
				t.Fatalf("unrelated notice %s gained a link: %+v", it.ID, it.Link)
			}
			if strings.Contains(itemJSON(t, raw, it.ID), `"link"`) {
				t.Fatalf("legacy/trade item %s serialized a link", it.ID)
			}
		}
		if it.Title == "客服已回复您的工单" {
			replyID = it.ID
			if it.Read || it.Kind != "system" || it.Link == nil || it.Link.Type != "support_ticket" || it.Link.TicketID != tk.ID {
				t.Fatalf("reply notice = %+v", it)
			}
			frag := itemJSON(t, raw, it.ID)
			if !strings.Contains(frag, `"ticketId":"`+tk.ID+`"`) {
				t.Fatalf("ticketId is not a JSON string: %s", frag)
			}
			if strings.Contains(frag, "deepLink") || strings.Contains(frag, "http") {
				t.Fatalf("reply notice carries a URL: %s", frag)
			}
			var oldShape struct {
				ID, Kind, Title, Body string
				Read                  bool
			}
			if err := json.Unmarshal([]byte(frag), &oldShape); err != nil || oldShape.ID != it.ID || oldShape.Title == "" {
				t.Fatalf("old clients cannot read the notice: %v %s", err, frag)
			}
		}
		if it.Title == "登录提醒" && it.Link != nil {
			t.Fatalf("security notice inferred a ticket: %+v", it.Link)
		}
	}
	if replyID == "" {
		t.Fatalf("reply notice missing: %s", raw)
	}

	bobList := doJSON(t, h, "GET", "/notifications?pageSize=50", bob.AccessToken, nil)
	if bobList.Code != 200 || strings.Contains(bobList.Body.String(), `"id":"`+replyID+`"`) || strings.Contains(bobList.Body.String(), `"ticketId":"`+tk.ID+`"`) {
		t.Fatalf("bob can see alice notice: %d %s", bobList.Code, bobList.Body.String())
	}
	if w := doJSON(t, h, "GET", "/support/tickets/"+tk.ID, bob.AccessToken, nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob opens ticket = %d", w.Code)
	}
	marked := doJSON(t, h, "POST", "/notifications/read", bob.AccessToken, map[string]any{"ids": []string{replyID}})
	var updated struct {
		Updated int `json:"updated"`
	}
	_ = json.Unmarshal(marked.Body.Bytes(), &updated)
	if marked.Code != 200 || updated.Updated != 0 {
		t.Fatalf("bob mark = %d %s", marked.Code, marked.Body.String())
	}

	if w := doJSON(t, h, "GET", "/support/tickets/"+tk.ID, alice.AccessToken, nil); w.Code != 200 {
		t.Fatalf("owner open = %d", w.Code)
	}
	if noticeRead(t, h, alice.AccessToken, replyID) {
		t.Fatal("opening the ticket marked the system notice read")
	}
	marked = doJSON(t, h, "POST", "/notifications/read", alice.AccessToken, map[string]any{"ids": []string{replyID}})
	updated.Updated = -1
	_ = json.Unmarshal(marked.Body.Bytes(), &updated)
	if marked.Code != 200 || updated.Updated != 1 {
		t.Fatalf("owner mark = %d %s", marked.Code, marked.Body.String())
	}
	if !noticeRead(t, h, alice.AccessToken, replyID) {
		t.Fatal("reply notice stayed unread")
	}
	if noticeRead(t, h, alice.AccessToken, strconv.FormatInt(legacyID, 10)) || noticeRead(t, h, alice.AccessToken, strconv.FormatInt(tradeID, 10)) {
		t.Fatal("marking the ticket notice also marked unrelated notices")
	}
}

func itemJSON(t *testing.T, raw, id string) string {
	t.Helper()
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(item, &head); err != nil {
			t.Fatal(err)
		}
		if head.ID == id {
			return string(item)
		}
	}
	t.Fatalf("item %s not in %s", id, raw)
	return ""
}

func noticeRead(t *testing.T, h http.Handler, token, id string) bool {
	t.Helper()
	w := doJSON(t, h, "GET", "/notifications?pageSize=50", token, nil)
	var page struct {
		Items []struct {
			ID   string `json:"id"`
			Read bool   `json:"read"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if it.ID == id {
			return it.Read
		}
	}
	t.Fatalf("notice %s missing", id)
	return false
}
