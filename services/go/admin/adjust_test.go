package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/shopspring/decimal"
)

const adjSecret = "JBSWY3DPEHPK3PXP"

type fakeAsset struct {
	mu    sync.Mutex
	bal   map[string]decimal.Decimal
	refs  map[string]string
	calls int
}

func (f *fakeAsset) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/internal/admin/users/") {
		w.Write([]byte(`{"uid":"x"}`))
		return
	}
	var req struct{ UserID, Asset, Account, Amount, Ref string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	k := req.UserID + "/" + req.Account + "/" + req.Asset
	if f.refs[req.Ref] != "" {
		fmt.Fprintf(w, `{"replayed":true}`)
		return
	}
	before := f.bal[k]
	after := before.Add(decimal.RequireFromString(req.Amount))
	if after.IsNegative() {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":"INSUFFICIENT_BALANCE"}}`))
		return
	}
	f.bal[k], f.refs[req.Ref] = after, k
	fmt.Fprintf(w, `{"freeBefore":%q,"freeAfter":%q}`, before.String(), after.String())
}

func adjustEnv(t *testing.T) (*Server, *fakeAsset, *actor, *actor) {
	dsn := os.Getenv("ADMIN_VIEWS_TEST_DSN")
	if dsn == "" {
		t.Skip("ADMIN_VIEWS_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	fa := &fakeAsset{bal: map[string]decimal.Decimal{}, refs: map[string]string{}}
	up := httptest.NewServer(fa)
	t.Cleanup(up.Close)
	mk := func(name string) *actor {
		var id int64
		n := fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
		if err := db.QueryRow(`INSERT INTO admin_users (username, password_hash, totp_secret, roles) VALUES ($1,'x',$2,'{finance_second}') RETURNING id`, n, adjSecret).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return &actor{ID: id, Username: n, Roles: []string{roleFinanceSecond}, TOTP: adjSecret}
	}
	_, _ = db.Exec(`DELETE FROM admin_settings WHERE key=$1`, adjustThresholdKey)
	s := NewServer(Config{AccountURL: up.URL, AssetURL: up.URL, MarketURL: "http://127.0.0.1:1", MMUserID: "mm:main",
		AdjustSecondAmount: decimal.RequireFromString("1000")}, db)
	return s, fa, mk("maker"), mk("checker")
}

func code(t *testing.T) string {
	c, err := totp.GenerateCode(adjSecret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func doAdj(h http.HandlerFunc, a *actor, path string, id int64, body any) (int, map[string]any) {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	if id > 0 {
		r.SetPathValue("id", strconv.FormatInt(id, 10))
	}
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, a))
	w := httptest.NewRecorder()
	h(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func adjOf(m map[string]any) map[string]any { x, _ := m["adjustment"].(map[string]any); return x }

func TestAdjustFlow(t *testing.T) {
	s, fa, maker, checker := adjustEnv(t)
	uid := fmt.Sprintf("u%d", time.Now().UnixNano())
	req := func(amt, ref, totp string) map[string]any {
		return map[string]any{"uid": uid, "asset": "USDT", "account": "spot", "amount": amt, "ref": ref, "reason": "补偿工单", "totpCode": totp}
	}
	ref := func(n string) string { return n + "-" + uid }

	// 2FA required: missing / wrong code, and an admin with no 2FA bound.
	if c, _ := doAdj(s.handleAdjustCreate, maker, "/", 0, req("10", ref("a"), "")); c != http.StatusUnauthorized {
		t.Fatalf("no code: %d", c)
	}
	no2fa := &actor{ID: maker.ID, Username: maker.Username}
	if c, _ := doAdj(s.handleAdjustCreate, no2fa, "/", 0, req("10", ref("a"), "123456")); c != http.StatusForbidden {
		t.Fatalf("unbound 2FA: %d", c)
	}
	// reason required
	r0 := req("10", ref("a"), code(t))
	r0["reason"] = " "
	if c, _ := doAdj(s.handleAdjustCreate, maker, "/", 0, r0); c != http.StatusBadRequest {
		t.Fatalf("no reason: %d", c)
	}

	// Below threshold: applied immediately.
	c, out := doAdj(s.handleAdjustCreate, maker, "/", 0, req("999.99", ref("a"), code(t)))
	if c != 200 || adjOf(out)["status"] != "applied" || adjOf(out)["freeAfter"] != "999.99" {
		t.Fatalf("small: %d %v", c, out)
	}
	// Idempotent create on ref: no second ledger call; param change -> 409.
	calls := fa.calls
	c, out = doAdj(s.handleAdjustCreate, maker, "/", 0, req("999.99", ref("a"), code(t)))
	if c != 200 || out["replayed"] != true || fa.calls != calls {
		t.Fatalf("replay: %d %v calls=%d", c, out, fa.calls)
	}
	if c, _ = doAdj(s.handleAdjustCreate, maker, "/", 0, req("5", ref("a"), code(t))); c != http.StatusConflict {
		t.Fatalf("ref conflict: %d", c)
	}

	// At threshold: pending, creator cannot approve, a different admin can.
	c, out = doAdj(s.handleAdjustCreate, maker, "/", 0, req("1000", ref("b"), code(t)))
	if c != http.StatusCreated || adjOf(out)["status"] != "pending_approval" || fa.calls != calls {
		t.Fatalf("big: %d %v", c, out)
	}
	id := int64(adjOf(out)["id"].(float64))
	if c, _ = doAdj(s.handleAdjustDecision(true), maker, "/", id, map[string]any{"totpCode": code(t)}); c != http.StatusForbidden {
		t.Fatalf("self approve: %d", c)
	}
	if _, err := s.db.Exec(`UPDATE balance_adjustments SET reviewed_by=created_by WHERE id=$1`, id); err == nil {
		t.Fatal("four-eyes constraint missing")
	}
	if c, _ = doAdj(s.handleAdjustDecision(true), checker, "/", id, map[string]any{"totpCode": "000000"}); c != http.StatusUnauthorized {
		t.Fatalf("approve bad 2FA: %d", c)
	}
	c, out = doAdj(s.handleAdjustDecision(true), checker, "/", id, map[string]any{"totpCode": code(t), "note": "核对无误"})
	if c != 200 || adjOf(out)["status"] != "applied" || adjOf(out)["freeAfter"] != "1999.99" {
		t.Fatalf("approve: %d %v", c, out)
	}
	if c, _ = doAdj(s.handleAdjustDecision(true), checker, "/", id, map[string]any{"totpCode": code(t)}); c != http.StatusConflict {
		t.Fatalf("double approve: %d", c)
	}

	// Unpriceable asset fails closed to review; reject needs a reason.
	rb := req("0.001", ref("c"), code(t))
	rb["asset"] = "BTC"
	c, out = doAdj(s.handleAdjustCreate, maker, "/", 0, rb)
	if c != http.StatusCreated || adjOf(out)["status"] != "pending_approval" {
		t.Fatalf("unpriced: %d %v", c, out)
	}
	id = int64(adjOf(out)["id"].(float64))
	if c, _ = doAdj(s.handleAdjustDecision(false), checker, "/", id, map[string]any{"totpCode": code(t)}); c != http.StatusBadRequest {
		t.Fatalf("reject w/o reason: %d", c)
	}
	if c, out = doAdj(s.handleAdjustDecision(false), checker, "/", id, map[string]any{"totpCode": code(t), "note": "无工单"}); c != 200 || adjOf(out)["status"] != "rejected" {
		t.Fatalf("reject: %d %v", c, out)
	}

	// Debit below zero is refused by the ledger and recorded as failed.
	c, out = doAdj(s.handleAdjustCreate, maker, "/", 0, req("-2000", ref("d"), code(t)))
	if c != http.StatusCreated {
		t.Fatalf("big debit should await review: %d %v", c, out)
	}
	id = int64(adjOf(out)["id"].(float64))
	c, out = doAdj(s.handleAdjustDecision(true), checker, "/", id, map[string]any{"totpCode": code(t)})
	if c != http.StatusBadRequest || adjOf(out)["status"] != "failed" || adjOf(out)["error"] != "INSUFFICIENT_BALANCE" {
		t.Fatalf("negative: %d %v", c, out)
	}

	// Audit trail has IP + before/after for the applied approval.
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM audit_logs WHERE target=$1 AND action IN ('adjust_apply','adjust_approve_apply') AND result='ok' AND before IS NOT NULL AND after IS NOT NULL AND ip IS NOT NULL`, "user:"+uid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit rows %d %v", n, err)
	}

	// List + filter.
	lr := httptest.NewRecorder()
	s.handleAdjustList(lr, httptest.NewRequest("GET", "/?uid="+uid+"&status=applied", nil))
	var lst struct {
		Items []adjustment `json:"items"`
		Total int          `json:"total"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &lst)
	if lr.Code != 200 || lst.Total != 2 {
		t.Fatalf("list: %d %s", lr.Code, lr.Body.String())
	}

	// Threshold is an admin setting.
	if c, _ = doAdj(s.handleAdjustConfigPut, checker, "/", 0, map[string]any{"secondReviewUsdt": "10", "reason": "收紧", "totpCode": code(t)}); c != 200 {
		t.Fatalf("config: %d", c)
	}
	defer s.db.Exec(`DELETE FROM admin_settings WHERE key=$1`, adjustThresholdKey) //nolint:errcheck
	if c, out = doAdj(s.handleAdjustCreate, maker, "/", 0, req("10", ref("e"), code(t))); c != http.StatusCreated {
		t.Fatalf("new threshold: %d %v", c, out)
	}
}
