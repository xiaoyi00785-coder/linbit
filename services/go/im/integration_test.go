package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

// newTestServer boots a fresh in-process Server pointed at $DATABASE_URL and
// at a stubbed asset upstream that always accepts transfers. Tests calling
// handlers go through the Server's mux (via Routes()) so both the auth
// middleware and path routing are exercised.
func newTestServer(t *testing.T) (*Server, http.Handler, *httptest.Server, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Skipf("DATABASE_URL set but unreachable (%v); skipping", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Stub asset service: accepts every /internal/user-transfer and records calls.
	asset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/user-transfer" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Header.Get("X-Internal-Token") != "secret" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(asset.Close)

	cfg := Config{
		JWTSecret:            "test-secret",
		IMTokenTTL:           time.Hour,
		AssetInternalURL:     asset.URL,
		InternalToken:        "secret",
		RedPacketExpireHours: 1,
		RedPacketMaxUSDT:     "20000",
		RedPacketMinUSDT:     "0.01",
		RedPacketMaxShares:   200,
		RecallWindow:         2 * time.Minute,
		DevMode:              true,
	}
	s := NewServer(cfg, NewStore(db), nil)
	return s, s.Routes(), asset, db
}

var testUIDSeq atomic.Int64

func testUID() string {
	return fmt.Sprintf("u-%d-%d", time.Now().UnixNano(), testUIDSeq.Add(1))
}

func asUser(req *http.Request, uid string) *http.Request {
	req.Header.Set("X-User-Id", uid)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// doJSON posts body as uid and returns status + decoded JSON.
func doJSON(t *testing.T, h http.Handler, method, path, uid string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req = asUser(req, uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestC2CFlow(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	alice, bob := testUID(), testUID()

	code, body := doJSON(t, h, http.MethodPost, "/im/conversations/"+bob+"/messages", alice, map[string]any{
		"kind":    "text",
		"payload": map[string]any{"text": "hi bob"},
	})
	if code != 200 {
		t.Fatalf("send: %d %v", code, body)
	}
	seq := int64(body["MsgSeq"].(float64))
	if seq != 1 {
		t.Fatalf("first seq should be 1, got %d", seq)
	}

	// History for bob should show that message.
	code, body = doJSON(t, h, http.MethodGet, "/im/conversations/"+alice+"/messages", bob, nil)
	if code != 200 {
		t.Fatalf("list: %d %v", code, body)
	}
	items, _ := body["MsgList"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 message, got %d", len(items))
	}
	// Bob's conversation list has the message with an unread counter.
	code, body = doJSON(t, h, http.MethodGet, "/im/conversations", bob, nil)
	if code != 200 {
		t.Fatalf("convs: %d %v", code, body)
	}
	convs, _ := body["items"].([]any)
	if len(convs) == 0 {
		t.Fatal("no conversations surfaced for bob")
	}
}

func TestGroupFlowAndRecall(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	owner, member := testUID(), testUID()

	// Create a group with member as initial.
	code, body := doJSON(t, h, http.MethodPost, "/im/groups", owner, map[string]any{
		"name":    "trader squad",
		"members": []string{member},
	})
	if code != 200 {
		t.Fatalf("create group: %d %v", code, body)
	}
	gid := int64(body["GroupId"].(float64))

	// Member sends a message.
	code, msg := doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/groups/%d/messages", gid), member, map[string]any{
		"kind":    "text",
		"payload": map[string]any{"text": "hello"},
	})
	if code != 200 {
		t.Fatalf("send: %d %v", code, msg)
	}
	mid := int64(msg["messageId"].(float64))

	// Owner recalls another member's message — should be forbidden.
	code, _ = doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/messages/%d/recall", mid), owner, nil)
	if code != http.StatusForbidden {
		t.Fatalf("owner recall of member msg should be forbidden, got %d", code)
	}
	// The sender may recall.
	code, _ = doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/messages/%d/recall", mid), member, nil)
	if code != 200 {
		t.Fatalf("recall: %d", code)
	}
	// Outside the window: deny.
	srv, h2, _, db := newTestServer(t)
	srv.now = func() time.Time { return time.Now().Add(-10 * time.Minute) }
	code, msg = doJSON(t, h2, http.MethodPost, "/im/conversations/"+member+"/messages", owner, map[string]any{
		"kind": "text", "payload": map[string]any{"text": "ancient"},
	})
	if code != 200 {
		t.Fatalf("ancient send: %d %v", code, msg)
	}
	oldID := int64(msg["messageId"].(float64))
	// Rewind the DB row so it looks old.
	_, _ = db.ExecContext(context.Background(), `UPDATE im_messages SET created_at = now() - interval '1 hour' WHERE id = $1`, oldID)
	srv.now = time.Now
	code, _ = doJSON(t, h2, http.MethodPost, fmt.Sprintf("/im/messages/%d/recall", oldID), owner, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("recall after window should 400, got %d", code)
	}
}

func TestGroupMute(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	owner, m := testUID(), testUID()
	_, body := doJSON(t, h, http.MethodPost, "/im/groups", owner, map[string]any{"name": "g", "members": []string{m}})
	gid := int64(body["GroupId"].(float64))

	// Mute the whole group.
	code, _ := doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/groups/%d/mute", gid), owner, map[string]any{"muted": true})
	if code != 200 {
		t.Fatalf("mute: %d", code)
	}
	// Regular member cannot talk.
	code, _ = doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/groups/%d/messages", gid), m, map[string]any{"kind": "text", "payload": map[string]any{"text": "silent"}})
	if code != http.StatusForbidden {
		t.Fatalf("muted member should 403, got %d", code)
	}
	// Owner still can.
	code, _ = doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/groups/%d/messages", gid), owner, map[string]any{"kind": "text", "payload": map[string]any{"text": "owner"}})
	if code != 200 {
		t.Fatalf("owner through mute: %d", code)
	}
}

func TestRedPacketCreateAndClaim(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	alice, bob := testUID(), testUID()
	_, body := doJSON(t, h, http.MethodPost, "/im/groups", alice, map[string]any{"name": "rp", "members": []string{bob}})
	gid := int64(body["GroupId"].(float64))

	// Alice creates a 10 USDT / 2 share equal redpacket.
	code, rp := doJSON(t, h, http.MethodPost, "/im/redpackets", alice, map[string]any{
		"groupId":     gid,
		"totalAmount": "10.00",
		"shares":      2,
		"mode":        "equal",
		"blessing":    "恭喜发财",
	})
	if code != 200 {
		t.Fatalf("create rp: %d %v", code, rp)
	}
	rpID := int64(rp["id"].(float64))

	// Alice claims one share, bob claims the other.
	code, claim := doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/redpackets/%d/claim", rpID), alice, nil)
	if code != 200 {
		t.Fatalf("claim1: %d %v", code, claim)
	}
	code, claim = doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/redpackets/%d/claim", rpID), bob, nil)
	if code != 200 {
		t.Fatalf("claim2: %d %v", code, claim)
	}
	// Second claim by bob should fail.
	code, claim = doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/redpackets/%d/claim", rpID), bob, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("double claim should 400, got %d %v", code, claim)
	}

	code, list := doJSON(t, h, http.MethodGet, fmt.Sprintf("/im/redpackets/%d/claims", rpID), alice, nil)
	if code != 200 {
		t.Fatalf("list claims: %d", code)
	}
	items, _ := list["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("want 2 claims, got %d", len(items))
	}
	// Both shares together must equal the total.
	sum := decimal.Zero
	for _, it := range items {
		m := it.(map[string]any)
		sum = sum.Add(decimal.RequireFromString(m["amount"].(string)))
	}
	if !sum.Equal(decimal.RequireFromString("10.00")) {
		t.Fatalf("claim sum %s != 10.00", sum.String())
	}
}

func TestRedPacketRequiresGroupMembership(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	alice, bob, carol := testUID(), testUID(), testUID()
	_, body := doJSON(t, h, http.MethodPost, "/im/groups", alice, map[string]any{"name": "rp", "members": []string{bob}})
	gid := int64(body["GroupId"].(float64))
	_, rp := doJSON(t, h, http.MethodPost, "/im/redpackets", alice, map[string]any{
		"groupId":     gid,
		"totalAmount": "1.00",
		"shares":      1,
		"mode":        "equal",
	})
	rpID := int64(rp["id"].(float64))
	code, _ := doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/redpackets/%d/claim", rpID), carol, nil)
	if code != http.StatusForbidden {
		t.Fatalf("non-member claim should 403, got %d", code)
	}
}

func TestConversationsListUnread(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	alice, bob := testUID(), testUID()

	for i := 0; i < 3; i++ {
		code, _ := doJSON(t, h, http.MethodPost, "/im/conversations/"+bob+"/messages", alice, map[string]any{
			"kind":    "text",
			"payload": map[string]any{"text": fmt.Sprintf("msg %d", i)},
		})
		if code != 200 {
			t.Fatalf("send %d", code)
		}
	}
	_, body := doJSON(t, h, http.MethodGet, "/im/conversations", bob, nil)
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 conv for bob, got %d", len(items))
	}
	entry := items[0].(map[string]any)
	if u := int(entry["unread"].(float64)); u != 3 {
		t.Fatalf("want unread=3, got %d", u)
	}
}

func TestForwardRequiresTarget(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	alice, bob := testUID(), testUID()
	_, msg := doJSON(t, h, http.MethodPost, "/im/conversations/"+bob+"/messages", alice, map[string]any{
		"kind":    "text",
		"payload": map[string]any{"text": "forward me"},
	})
	mid := int64(msg["messageId"].(float64))
	code, _ := doJSON(t, h, http.MethodPost, fmt.Sprintf("/im/messages/%d/forward", mid), alice, map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("missing target should 400, got %d", code)
	}
}

func TestHealthz(t *testing.T) {
	_, h, _, _ := newTestServer(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("healthz: %d %q", w.Code, w.Body.String())
	}
}
