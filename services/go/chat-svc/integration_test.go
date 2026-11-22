package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Integration test against a live Postgres. Set CHAT_TEST_DB to a DSN to run it;
// otherwise it skips.
func testServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("CHAT_TEST_DB")
	if dsn == "" {
		t.Skip("CHAT_TEST_DB not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Clean slate for deterministic assertions across repeated runs.
	if _, err := db.Exec(`TRUNCATE cs_messages, cs_conversations RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// Make rate limits non-blocking for the happy-path flow.
	if _, err := db.Exec(`UPDATE chat_config SET cfg='{"minIntervalMs":0,"maxLength":200,"minKycLevel":0,"blockLinks":true,"blockContacts":true,"historyLimitMax":100,"generalRoom":true,"futuresRooms":true}' WHERE id=1`); err != nil {
		t.Fatalf("cfg: %v", err)
	}
	cfg := Config{InternalToken: "testtok", ChatEnabled: true, CSEnabled: true, CSGuestEnabled: true,
		GuestMinIntervalMs: 0, GuestMaxPerHour: 1000, UploadDir: t.TempDir(), UploadMaxBytes: 1 << 20}
	srv := NewServer(cfg, NewStore(db))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts, db
}

func doJSON(t *testing.T, method, url string, body any, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestIntegrationGuestFlow(t *testing.T) {
	ts, db := testServer(t)
	defer db.Close()
	gk := "guestkey-abc-123456"

	// 1. Guest opens a conversation.
	code, conv := doJSON(t, "POST", ts.URL+"/cs/guest/conversations", map[string]any{"guestKey": gk, "subject": "充值没到账"}, nil)
	if code != 200 {
		t.Fatalf("guest open status=%d body=%v", code, conv)
	}
	token, _ := conv["pubToken"].(string)
	if token == "" {
		t.Fatalf("no pubToken: %v", conv)
	}

	// 2. Guest sends a text message.
	code, m := doJSON(t, "POST", ts.URL+"/cs/guest/conversations/"+token+"/messages", map[string]any{"guestKey": gk, "clientId": "c1", "text": "你好，我的充值没到账"}, nil)
	if code != 201 {
		t.Fatalf("guest send status=%d body=%v", code, m)
	}
	// 3. Idempotent replay.
	code2, m2 := doJSON(t, "POST", ts.URL+"/cs/guest/conversations/"+token+"/messages", map[string]any{"guestKey": gk, "clientId": "c1", "text": "你好，我的充值没到账"}, nil)
	if code2 != 200 || m2["id"] != m["id"] {
		t.Fatalf("idempotency failed: code=%d id1=%v id2=%v", code2, m["id"], m2["id"])
	}
	// 4. Link filter blocks.
	code, bad := doJSON(t, "POST", ts.URL+"/cs/guest/conversations/"+token+"/messages", map[string]any{"guestKey": gk, "clientId": "c2", "text": "加我 http://evil.example"}, nil)
	if code != 400 || bad["code"] != "CHAT_LINK_BLOCKED" {
		t.Fatalf("link should be blocked, got code=%d body=%v", code, bad)
	}

	// 5. Admin sees the conversation with staff-unread.
	ah := map[string]string{"X-Internal-Token": "testtok"}
	code, list := doJSON(t, "GET", ts.URL+"/internal/cs/conversations?status=open", nil, ah)
	if code != 200 {
		t.Fatalf("admin list status=%d", code)
	}
	items, _ := list["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("admin list empty: %v", list)
	}
	first := items[0].(map[string]any)
	convID := first["id"]
	if first["unreadStaff"].(float64) < 1 {
		t.Fatalf("expected unreadStaff>=1, got %v", first["unreadStaff"])
	}

	// 6. Admin replies.
	idStr := jsonNum(convID)
	code, rep := doJSON(t, "POST", ts.URL+"/internal/cs/conversations/"+idStr+"/reply", map[string]any{"agent": "agent007", "clientId": "a1", "text": "您好，正在为您核实"}, ah)
	if code != 201 {
		t.Fatalf("reply status=%d body=%v", code, rep)
	}

	// 7. Guest fetches messages: should see user + staff in order.
	code, msgs := doJSON(t, "GET", ts.URL+"/cs/guest/conversations/"+token+"/messages", nil, nil)
	if code != 200 {
		t.Fatalf("guest messages status=%d", code)
	}
	arr, _ := msgs["items"].([]any)
	if len(arr) < 2 {
		t.Fatalf("expected >=2 messages, got %d: %v", len(arr), msgs)
	}
	last := arr[len(arr)-1].(map[string]any)
	if last["senderKind"] != "staff" || last["sender"] != "客服" {
		t.Fatalf("last message should be staff/客服: %v", last)
	}

	// 8. Claim to a logged-in user.
	uh := map[string]string{"X-User-Id": "80000010"}
	code, claim := doJSON(t, "POST", ts.URL+"/cs/conversations/claim", map[string]any{"guestKey": gk}, uh)
	if code != 200 || claim["claimed"].(float64) < 1 {
		t.Fatalf("claim failed: code=%d body=%v", code, claim)
	}
	// 9. The user now lists the conversation.
	code, ulist := doJSON(t, "GET", ts.URL+"/cs/conversations", nil, uh)
	uitems, _ := ulist["items"].([]any)
	if code != 200 || len(uitems) == 0 {
		t.Fatalf("user list after claim empty: code=%d body=%v", code, ulist)
	}
}

func jsonNum(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	// strip possible exponent for small ints
	if i := bytesIndex(s, '.'); i >= 0 {
		s = s[:i]
	}
	return s
}

func bytesIndex(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
