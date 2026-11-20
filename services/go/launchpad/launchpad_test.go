package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

// --- test harness ---

// fakeAsset mirrors asset's /internal/system-transfer: it keeps in-memory
// spot balances per (user, asset), rejects duplicate refs (so the service's
// idempotency is actually exercised) and 400s on insufficient debit.
type fakeAsset struct {
	srv      *httptest.Server
	mu       sync.Mutex
	balances map[string]decimal.Decimal
	seenRefs map[string]bool
	calls    int
}

func newFakeAsset() *fakeAsset {
	fa := &fakeAsset{balances: map[string]decimal.Decimal{}, seenRefs: map[string]bool{}}
	fa.srv = httptest.NewServer(http.HandlerFunc(fa.handle))
	return fa
}

func (f *fakeAsset) key(uid, asset string) string { return uid + "|" + asset }

func (f *fakeAsset) credit(uid, asset string, amount decimal.Decimal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balances[f.key(uid, asset)] = f.balances[f.key(uid, asset)].Add(amount)
}

func (f *fakeAsset) balance(uid, asset string) decimal.Decimal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.balances[f.key(uid, asset)]
}

func (f *fakeAsset) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/internal/system-transfer" {
		http.NotFound(w, r)
		return
	}
	var req struct {
		UserID    string `json:"userId"`
		Asset     string `json:"asset"`
		Amount    string `json:"amount"`
		Account   string `json:"account"`
		Direction string `json:"direction"`
		Ref       string `json:"ref"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	amt, err := decimal.NewFromString(req.Amount)
	if err != nil {
		http.Error(w, "bad amount", 400)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.seenRefs[req.Ref] {
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
		return
	}
	f.seenRefs[req.Ref] = true
	k := f.key(req.UserID, req.Asset)
	switch req.Direction {
	case "debit":
		have := f.balances[k]
		if have.LessThan(amt) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"INSUFFICIENT_BALANCE","message":"insufficient balance"}`))
			return
		}
		f.balances[k] = have.Sub(amt)
	case "credit":
		f.balances[k] = f.balances[k].Add(amt)
	default:
		http.Error(w, "bad direction", 400)
		return
	}
	w.WriteHeader(200)
	w.Write([]byte(`{}`))
}

var testSeq atomic.Int64

func testUser() string {
	return fmt.Sprintf("u-%d-%04d", time.Now().UnixNano(), testSeq.Add(1)%10000)
}

func newTestServer(t *testing.T) (*Server, *fakeAsset) {
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
	fa := newFakeAsset()
	t.Cleanup(fa.srv.Close)
	cfg := Config{AssetURL: fa.srv.URL, InternalToken: "test-token", DevMode: true}
	srv := NewServer(cfg, db)
	return srv, fa
}

// advance moves the service clock forward and runs one tick.
func advance(t *testing.T, s *Server, hours float64) {
	t.Helper()
	s.offset.Add(int64(hours * float64(time.Hour)))
	s.Tick(context.Background())
}

// seedProject inserts a project whose window starts ~1h in the past so a
// subscribe right after this call always lands inside it.
func seedProject(t *testing.T, s *Server, id, symbol, base string, price, supply, minSub string, maxSub *string, windowHours, settleAfterHours, distAfterHours int) {
	t.Helper()
	ctx := context.Background()
	p, _ := decimal.NewFromString(price)
	sup, _ := decimal.NewFromString(supply)
	mn, _ := decimal.NewFromString(minSub)
	var mx *decimal.Decimal
	if maxSub != nil {
		v, _ := decimal.NewFromString(*maxSub)
		mx = &v
	}
	now := s.now()
	start := now.Add(-time.Hour)
	end := start.Add(time.Duration(windowHours) * time.Hour)
	settle := end.Add(time.Duration(settleAfterHours) * time.Hour)
	dist := settle.Add(time.Duration(distAfterHours) * time.Hour)
	_, err := s.db.ExecContext(ctx, `INSERT INTO launchpad_projects
		(id, symbol, name, base_asset, price, total_supply, min_sub, max_sub,
		 start_at, end_at, settle_at, distribute_at, status, rules_md, sort)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'open','# Test',0)
		ON CONFLICT (id) DO UPDATE SET price=$5, total_supply=$6, min_sub=$7, max_sub=$8,
			start_at=$9, end_at=$10, settle_at=$11, distribute_at=$12, status='open'`,
		id, symbol, symbol+" launch", base, p, sup, mn, mx, start, end, settle, dist)
	if err != nil {
		t.Fatalf("seed project %s: %v", id, err)
	}
}

func loadSub(t *testing.T, s *Server, pid, uid string) (status string, amount, result, used, refund decimal.Decimal) {
	t.Helper()
	var r, u, rf sql.NullString
	err := s.db.QueryRowContext(context.Background(), `SELECT status, amount, result_amount::text, used_base::text, refund_amount::text
		FROM launchpad_subscriptions WHERE project_id=$1 AND uid=$2`, pid, uid).Scan(&status, &amount, &r, &u, &rf)
	if err != nil {
		t.Fatalf("load sub %s/%s: %v", pid, uid, err)
	}
	if r.Valid {
		result, _ = decimal.NewFromString(r.String)
	}
	if u.Valid {
		used, _ = decimal.NewFromString(u.String)
	}
	if rf.Valid {
		refund, _ = decimal.NewFromString(rf.String)
	}
	return
}

func projectStatus(t *testing.T, s *Server, pid string) string {
	t.Helper()
	var st string
	if err := s.db.QueryRowContext(context.Background(), `SELECT status FROM launchpad_projects WHERE id=$1`, pid).Scan(&st); err != nil {
		t.Fatalf("project status %s: %v", pid, err)
	}
	return st
}

// --- tests ---

func TestSubscribeLocksSpotAndCapsAtMax(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	pid := "test-sub-lock-" + fmt.Sprint(time.Now().UnixNano())
	mx := "500"
	seedProject(t, s, pid, "TST1", "USDT", "0.5", "1000", "10", &mx, 24, 1, 1)

	fa.credit(uid, "USDT", decimal.RequireFromString("1000"))

	if _, err := s.subscribe(ctx, uid, pid, "100"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if got := fa.balance(uid, "USDT"); !got.Equal(decimal.RequireFromString("900")) {
		t.Fatalf("spot after 1st sub = %s, want 900", got)
	}

	// Below min: rejected, spot unchanged.
	before := fa.balance(uid, "USDT")
	if _, err := s.subscribe(ctx, uid, pid, "1"); err == nil {
		t.Fatal("subscribe below min should fail")
	}
	if !fa.balance(uid, "USDT").Equal(before) {
		t.Fatalf("spot changed after rejected sub: %s -> %s", before, fa.balance(uid, "USDT"))
	}

	// Add more — now at 450, still under 500 max.
	if _, err := s.subscribe(ctx, uid, pid, "350"); err != nil {
		t.Fatalf("second subscribe: %v", err)
	}
	// Now at 450; one more of 100 would exceed 500.
	if _, err := s.subscribe(ctx, uid, pid, "100"); err == nil {
		t.Fatal("subscribe over user max should fail")
	}
	st, amount, _, _, _ := loadSub(t, s, pid, uid)
	if st != "holding" || !amount.Equal(decimal.RequireFromString("450")) {
		t.Fatalf("sub after caps: status=%s amount=%s (want holding/450)", st, amount)
	}
}

func TestCancelRefundsLockedFunds(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	pid := "test-cancel-" + fmt.Sprint(time.Now().UnixNano())
	seedProject(t, s, pid, "TST2", "USDT", "1", "1000", "10", nil, 24, 1, 1)

	fa.credit(uid, "USDT", decimal.RequireFromString("500"))
	if _, err := s.subscribe(ctx, uid, pid, "300"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if got := fa.balance(uid, "USDT"); !got.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("spot after sub = %s, want 200", got)
	}

	// Partial cancel: 100 back, 200 remains locked.
	s.mu.Lock()
	_, _, _, err := doCancel(ctx, s, uid, pid, "100", false)
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("partial cancel: %v", err)
	}
	if got := fa.balance(uid, "USDT"); !got.Equal(decimal.RequireFromString("300")) {
		t.Fatalf("spot after partial cancel = %s, want 300", got)
	}

	// Full cancel of remainder.
	s.mu.Lock()
	_, _, _, err = doCancel(ctx, s, uid, pid, "", true)
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("full cancel: %v", err)
	}
	if got := fa.balance(uid, "USDT"); !got.Equal(decimal.RequireFromString("500")) {
		t.Fatalf("spot after full cancel = %s, want 500", got)
	}
	st, amount, _, _, _ := loadSub(t, s, pid, uid)
	if st != "cancelled" || !amount.IsZero() {
		t.Fatalf("after full cancel: status=%s amount=%s", st, amount)
	}
}

// doCancel is a test helper that calls the HTTP layer so we exercise the real
// transaction / refund path.
func doCancel(ctx context.Context, s *Server, uid, pid, amount string, all bool) (string, decimal.Decimal, decimal.Decimal, error) {
	body := strings.NewReader(fmt.Sprintf(`{"amount":%q,"all":%v}`, amount, all))
	req := httptest.NewRequest("POST", "/launchpad/projects/"+pid+"/cancel", body)
	req.SetPathValue("id", pid)
	req.Header.Set("X-User-Id", uid)
	w := httptest.NewRecorder()
	s.mu.Unlock() // handleCancel takes mu; caller re-acquires on return
	s.handleCancel(w, req, uid)
	s.mu.Lock()
	if w.Code != 200 {
		return "", decimal.Zero, decimal.Zero, fmt.Errorf("cancel HTTP %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return "", decimal.Zero, decimal.Zero, nil
}

func TestUndersubscribedFillsFullAllocation(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	pid := "test-under-" + fmt.Sprint(time.Now().UnixNano())
	// 1000 tokens @ 1 USDT each = 1000 USDT cap. Window 2h.
	seedProject(t, s, pid, "UND", "USDT", "1", "1000", "10", nil, 2, 1, 1)
	fa.credit(uid, "USDT", decimal.RequireFromString("1000"))
	if _, err := s.subscribe(ctx, uid, pid, "200"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Advance ~3h so the window closes + settle_at passes + distribute_at passes.
	// Need at least one snapshot in the window; Tick on seed+now will take one.
	s.Tick(ctx)
	advance(t, s, 1) // 1h later: still open, one more snapshot
	advance(t, s, 2) // past end + settle
	advance(t, s, 2) // past distribute
	st, amount, result, used, refund := loadSub(t, s, pid, uid)
	if st != "distributed" {
		t.Fatalf("expected distributed, got %s", st)
	}
	if !result.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("result = %s, want 200 tokens", result)
	}
	if !used.Equal(decimal.RequireFromString("200")) || !refund.IsZero() {
		t.Fatalf("used=%s refund=%s, want 200/0", used, refund)
	}
	if !amount.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("lock amount tracked = %s", amount)
	}
	if got := fa.balance(uid, "UND"); !got.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("UND balance = %s, want 200", got)
	}
	if projectStatus(t, s, pid) != "distributed" {
		t.Fatalf("project not distributed")
	}
}

func TestOversubscribedProratesAndRefunds(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	u1, u2 := testUser(), testUser()
	pid := "test-over-" + fmt.Sprint(time.Now().UnixNano())
	// 100 tokens @ 1 USDT = 100 USDT cap. Two users subscribe 150 each -> 300 avg.
	// ratio = 100 / 300 = 1/3. Each gets used = 50, refund = 100, tokens = 50.
	seedProject(t, s, pid, "OVR", "USDT", "1", "100", "10", nil, 2, 1, 1)
	fa.credit(u1, "USDT", decimal.RequireFromString("500"))
	fa.credit(u2, "USDT", decimal.RequireFromString("500"))
	if _, err := s.subscribe(ctx, u1, pid, "150"); err != nil {
		t.Fatalf("u1 subscribe: %v", err)
	}
	if _, err := s.subscribe(ctx, u2, pid, "150"); err != nil {
		t.Fatalf("u2 subscribe: %v", err)
	}
	s.Tick(ctx)         // first snapshot
	advance(t, s, 1)    // second snapshot
	advance(t, s, 2)    // settle
	advance(t, s, 2)    // distribute

	for _, u := range []string{u1, u2} {
		st, _, result, used, refund := loadSub(t, s, pid, u)
		if st != "distributed" {
			t.Fatalf("%s status=%s", u, st)
		}
		if !result.Equal(decimal.RequireFromString("50")) {
			t.Fatalf("%s result=%s want 50", u, result)
		}
		if !used.Equal(decimal.RequireFromString("50")) || !refund.Equal(decimal.RequireFromString("100")) {
			t.Fatalf("%s used=%s refund=%s want 50/100", u, used, refund)
		}
		// Spot: debited 150 (lock), credited 50 OVR + 100 USDT refund.
		if got := fa.balance(u, "OVR"); !got.Equal(decimal.RequireFromString("50")) {
			t.Fatalf("%s OVR balance = %s", u, got)
		}
		if got := fa.balance(u, "USDT"); !got.Equal(decimal.RequireFromString("450")) {
			t.Fatalf("%s USDT balance after distribute = %s, want 450", u, got)
		}
	}
	if projectStatus(t, s, pid) != "distributed" {
		t.Fatalf("project not distributed")
	}
}

func TestSubscribeOutsideWindow(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	pid := "test-closed-" + fmt.Sprint(time.Now().UnixNano())
	// Window was 1h starting 1h ago — it is already closed.
	seedProject(t, s, pid, "CLS", "USDT", "1", "100", "10", nil, 1, 1, 1)
	fa.credit(uid, "USDT", decimal.RequireFromString("100"))

	if _, err := s.subscribe(ctx, uid, pid, "50"); err == nil {
		t.Fatal("subscribe after window close should fail")
	}
	if got := fa.balance(uid, "USDT"); !got.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("spot changed after rejected sub: %s", got)
	}
}

func TestAdminCancelRefundsEveryone(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	u1, u2 := testUser(), testUser()
	pid := "test-admcancel-" + fmt.Sprint(time.Now().UnixNano())
	seedProject(t, s, pid, "ADM", "USDT", "1", "100", "10", nil, 24, 1, 1)
	fa.credit(u1, "USDT", decimal.RequireFromString("100"))
	fa.credit(u2, "USDT", decimal.RequireFromString("100"))
	if _, err := s.subscribe(ctx, u1, pid, "80"); err != nil {
		t.Fatalf("u1 sub: %v", err)
	}
	if _, err := s.subscribe(ctx, u2, pid, "60"); err != nil {
		t.Fatalf("u2 sub: %v", err)
	}
	s.mu.Lock()
	err := s.cancelProject(ctx, pid)
	s.mu.Unlock()
	if err != nil {
		t.Fatalf("admin cancel: %v", err)
	}
	if projectStatus(t, s, pid) != "cancelled" {
		t.Fatal("project should be cancelled")
	}
	// Both users refunded.
	if got := fa.balance(u1, "USDT"); !got.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("u1 refund: %s", got)
	}
	if got := fa.balance(u2, "USDT"); !got.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("u2 refund: %s", got)
	}
}

func TestDistributeIsIdempotent(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	pid := "test-idem-" + fmt.Sprint(time.Now().UnixNano())
	seedProject(t, s, pid, "IDM", "USDT", "1", "1000", "10", nil, 2, 1, 1)
	fa.credit(uid, "USDT", decimal.RequireFromString("500"))
	if _, err := s.subscribe(ctx, uid, pid, "200"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	advance(t, s, 1)
	advance(t, s, 2) // settle
	advance(t, s, 2) // distribute
	callsAfter := fa.calls
	// Run distribute again on a settled/distributed project: no new payouts.
	s.mu.Lock()
	// setting status back to 'settled' would re-run; we just verify Tick doesn't double-pay
	s.mu.Unlock()
	s.Tick(ctx)
	if fa.calls != callsAfter {
		t.Fatalf("Tick after distribute added %d asset calls", fa.calls-callsAfter)
	}
	// Payouts: 200 IDM + 0 refund (full fill). No USDT change.
	if got := fa.balance(uid, "IDM"); !got.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("IDM = %s after double-tick", got)
	}
}

func TestSnapshotAverage(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	pid := "test-avg-" + fmt.Sprint(time.Now().UnixNano())
	// Window 6h so advances 1/1/1/1 stay inside it.
	seedProject(t, s, pid, "AVG", "USDT", "1", "1000", "10", nil, 6, 1, 1)
	fa.credit(uid, "USDT", decimal.RequireFromString("1000"))
	if _, err := s.subscribe(ctx, uid, pid, "100"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	s.Tick(ctx) // snapshot 1 @ 100
	advance(t, s, 1) // snapshot 2 @ 100
	// Add 200 more → now 300.
	if _, err := s.subscribe(ctx, uid, pid, "200"); err != nil {
		t.Fatalf("subscribe 2: %v", err)
	}
	advance(t, s, 1) // snapshot 3 @ 300
	advance(t, s, 1) // snapshot 4 @ 300
	// Advance past end + settle + distribute.
	advance(t, s, 6)
	_, _, result, used, refund := loadSub(t, s, pid, uid)
	// avg = (100+100+300+300)/4 = 200. Cap = 1000 > avg, so used = avg = 200.
	// Tokens = 200, refund = locked (300) - used (200) = 100.
	if !used.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("used = %s, want 200", used)
	}
	if !result.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("result = %s, want 200", result)
	}
	if !refund.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("refund = %s, want 100", refund)
	}
}

// TestHTTPEndToEnd drives the full admin-create → subscribe → settle →
// distribute flow through the HTTP mux, the same way the gateway would.
func TestHTTPEndToEnd(t *testing.T) {
	s, fa := newTestServer(t)
	mux := s.Routes()
	pid := "e2e-" + fmt.Sprint(time.Now().UnixNano())
	u1, u2 := testUser(), testUser()

	// Admin creates the project (window opens 1 min in the past so subscribe
	// lands inside it; settle_at 1h out, distribute 1h after that).
	now := s.now()
	body := fmt.Sprintf(`{"id":%q,"symbol":"E2E","name":"E2E Launch","baseAsset":"USDT",
		"price":"2","totalSupply":"100","minSub":"10","startAt":%d,"endAt":%d,
		"settleAt":%d,"distributeAt":%d,"rulesMd":"# E2E rules"}`,
		pid, now.Add(-time.Minute).UnixMilli(), now.Add(time.Hour).UnixMilli(),
		now.Add(2*time.Hour).UnixMilli(), now.Add(3*time.Hour).UnixMilli())
	req := httptest.NewRequest("POST", "/internal/launchpad/projects", strings.NewReader(body))
	req.Header.Set("X-Internal-Token", "test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}

	// Two authenticated subscriptions, each big enough to oversubscribe
	// against the 200 USDT cap (totalSupply * price).
	fa.credit(u1, "USDT", decimal.RequireFromString("500"))
	fa.credit(u2, "USDT", decimal.RequireFromString("500"))
	for _, u := range []string{u1, u2} {
		req := httptest.NewRequest("POST", "/launchpad/projects/"+pid+"/subscribe", strings.NewReader(`{"amount":"300"}`))
		req.SetPathValue("id", pid)
		req.Header.Set("X-User-Id", u)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s subscribe: %d %s", u, w.Code, w.Body.String())
		}
	}
	// Spot drained by lock.
	if got := fa.balance(u1, "USDT"); !got.Equal(decimal.RequireFromString("200")) {
		t.Fatalf("u1 USDT after lock = %s, want 200", got)
	}

	// Take an initial snapshot, then jump the clock past settle + distribute.
	s.Tick(context.Background())
	advance(t, s, 3) // past end + settle
	advance(t, s, 2) // past distribute

	// Admin lists projects — must now report distributed.
	req = httptest.NewRequest("GET", "/internal/launchpad/projects", nil)
	req.Header.Set("X-Internal-Token", "test-token")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rawStatus":"distributed"`) {
		t.Fatalf("admin list after distribute: %d %s", w.Code, w.Body.String())
	}

	// Each user: result should be 50 tokens (200 cap / 2 users = 100 USDT each at price 2 = 50 E2E).
	for _, u := range []string{u1, u2} {
		req := httptest.NewRequest("GET", "/launchpad/subscriptions", nil)
		req.Header.Set("X-User-Id", u)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"distributed"`) {
			t.Fatalf("%s subscriptions list: %d %s", u, w.Code, w.Body.String())
		}
		if got := fa.balance(u, "E2E"); !got.Equal(decimal.RequireFromString("50")) {
			t.Fatalf("%s E2E balance after distribute = %s, want 50", u, got)
		}
		if got := fa.balance(u, "USDT"); !got.Equal(decimal.RequireFromString("400")) {
			t.Fatalf("%s USDT after refund = %s, want 400 (500 - 100 used)", u, got)
		}
	}

	// Rules endpoint is public.
	req = httptest.NewRequest("GET", "/launchpad/projects/"+pid+"/rules", nil)
	req.SetPathValue("id", pid)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "E2E rules") {
		t.Fatalf("rules: %d %s", w.Code, w.Body.String())
	}
}

func TestPublicCatalogueRouting(t *testing.T) {
	s, _ := newTestServer(t)
	pid := "test-public-" + fmt.Sprint(time.Now().UnixNano())
	seedProject(t, s, pid, "PUB", "USDT", "1", "100", "10", nil, 24, 1, 1)
	// Public GET works with no X-User-Id.
	req := httptest.NewRequest("GET", "/launchpad/projects", nil)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("GET /launchpad/projects: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), pid) {
		t.Fatalf("project list missing %s: %s", pid, w.Body.String())
	}
	// Subscribe requires X-User-Id.
	req = httptest.NewRequest("POST", "/launchpad/projects/"+pid+"/subscribe", strings.NewReader(`{"amount":"10"}`))
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("subscribe without auth: %d", w.Code)
	}
}
