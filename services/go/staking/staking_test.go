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

// fakeAsset is a stand-in for the asset service's /internal/system-transfer
// endpoint. It keeps an in-memory spot balance per (user, asset) and rejects
// duplicate refs so the service's idempotency is actually exercised.
type fakeAsset struct {
	srv      *httptest.Server
	mu       sync.Mutex
	balances map[string]decimal.Decimal // "uid|asset"
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
		// Idempotent replay: pretend success, change nothing.
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

// --- DB harness ---

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

// advance moves the service clock forward by the given hours and runs one tick.
func advance(t *testing.T, s *Server, hours float64) {
	t.Helper()
	s.offset.Add(int64(hours * float64(time.Hour)))
	s.Tick(context.Background())
}

// seedProduct inserts (or refreshes) a product for a test. The test-specific
// id keeps parallel tests from stepping on each other.
func seedProduct(t *testing.T, s *Server, id, asset string, apr float64, unbondDays int) {
	t.Helper()
	// Validator must be unique per (asset, validator), so derive it from the
	// test-scoped id — parallel tests seeding the same asset would otherwise
	// trip the s_products_asset_validator_key constraint.
	validator := "TestValidator-" + id
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO s_products
		(id, asset, chain, validator, apr, unbonding_days, min_amount, commission, status, sort)
		VALUES ($1,$2,'TestChain',$3,$4,$5,0.0001,0,'online',999)
		ON CONFLICT (id) DO UPDATE SET validator=$3, apr=$4, unbonding_days=$5, status='online'`,
		id, asset, validator, apr, unbondDays)
	if err != nil {
		t.Fatalf("seed product %s: %v", id, err)
	}
}

// loadPosition fetches a position by id (no row lock).
func loadPosition(t *testing.T, s *Server, id int64) *position {
	t.Helper()
	p := &position{}
	err := s.db.QueryRowContext(context.Background(), `SELECT `+positionCols+` FROM s_positions WHERE id=$1`, id).
		Scan(&p.ID, &p.UID, &p.ProductID, &p.Asset, &p.Amount, &p.APRSnapshot, &p.Status, &p.StartedAt,
			&p.UnbondAt, &p.ReleasedAt, &p.LastSettleDay, &p.PendingRewards, &p.ClaimedRewards)
	if err != nil {
		t.Fatalf("load position %d: %v", id, err)
	}
	return p
}

// --- tests ---

func TestStakeDebitsSpot(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	seedProduct(t, s, "test-stake-debit", "ETH", 0.0365, 7) // 3.65% apr = 0.0001 ETH/day per 1 ETH

	fa.credit(uid, "ETH", decimal.RequireFromString("10"))

	out, err := s.stake(ctx, uid, stakeReq{Asset: "ETH", Validator: "test-stake-debit", Amount: "2.5"})
	if err != nil {
		t.Fatalf("stake: %v", err)
	}
	pid := out["positionId"].(int64)
	if got := fa.balance(uid, "ETH"); !got.Equal(decimal.RequireFromString("7.5")) {
		t.Fatalf("spot after stake = %s, want 7.5", got)
	}
	pos := loadPosition(t, s, pid)
	if pos.Status != "active" || !pos.Amount.Equal(decimal.RequireFromString("2.5")) {
		t.Fatalf("position state wrong: %+v", pos)
	}

	// Under min: rejected with no debit, no position.
	before := fa.balance(uid, "ETH")
	if _, err := s.stake(ctx, uid, stakeReq{Asset: "ETH", Validator: "test-stake-debit", Amount: "0.00001"}); err == nil {
		t.Fatal("stake below minimum should fail")
	}
	if !fa.balance(uid, "ETH").Equal(before) {
		t.Fatalf("spot changed after rejected stake: %s -> %s", before, fa.balance(uid, "ETH"))
	}

	// Insufficient spot: rejected and the position is rolled back.
	if _, err := s.stake(ctx, uid, stakeReq{Asset: "ETH", Validator: "test-stake-debit", Amount: "9999"}); err == nil {
		t.Fatal("stake above spot balance should fail")
	}
	var count int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM s_positions WHERE uid=$1 AND product_id='test-stake-debit'`, uid).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 position after failed stake, got %d", count)
	}
}

func TestDailyRewardsAndClaim(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	// 36.5% APR = 0.001 per day on 1 unit.
	seedProduct(t, s, "test-daily-rewards", "SOL", 0.365, 3)

	fa.credit(uid, "SOL", decimal.RequireFromString("100"))
	out, err := s.stake(ctx, uid, stakeReq{Asset: "SOL", Validator: "test-daily-rewards", Amount: "10"})
	if err != nil {
		t.Fatalf("stake: %v", err)
	}
	pid := out["positionId"].(int64)

	// Same day: no reward yet.
	s.Tick(ctx)
	if p := loadPosition(t, s, pid); !p.PendingRewards.IsZero() {
		t.Fatalf("same-day pending rewards = %s, want 0", p.PendingRewards)
	}

	// 25 hours = at least one full UTC day boundary.
	advance(t, s, 25)
	p := loadPosition(t, s, pid)
	if p.PendingRewards.Sign() <= 0 {
		t.Fatalf("after 25h expected positive pending rewards, got %s", p.PendingRewards)
	}
	daily := p.PendingRewards
	// Expected = floor8(10 * 0.365 / 365) = 0.01 for one day.
	if !daily.Equal(decimal.RequireFromString("0.01000000")) {
		t.Fatalf("daily reward = %s, want 0.01", daily)
	}

	// Advance another ~24h: one more day's worth.
	advance(t, s, 24)
	p = loadPosition(t, s, pid)
	if !p.PendingRewards.Equal(decimal.RequireFromString("0.02000000")) {
		t.Fatalf("after 2d pending = %s, want 0.02", p.PendingRewards)
	}

	// Claim: moves pending -> spot, zeroes pending, marks rows claimed.
	spotBefore := fa.balance(uid, "SOL")
	cl, err := s.claim(ctx, uid, pid)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !fa.balance(uid, "SOL").Sub(spotBefore).Equal(decimal.RequireFromString("0.02")) {
		t.Fatalf("spot delta after claim = %s, want 0.02", fa.balance(uid, "SOL").Sub(spotBefore))
	}
	p = loadPosition(t, s, pid)
	if !p.PendingRewards.IsZero() || !p.ClaimedRewards.Equal(decimal.RequireFromString("0.02000000")) {
		t.Fatalf("after claim pending=%s claimed=%s", p.PendingRewards, p.ClaimedRewards)
	}
	if n := len(cl["items"].([]map[string]any)); n != 1 {
		t.Fatalf("claim returned %d items, want 1", n)
	}

	// Double-claim: nothing to pay out, spot unchanged, no new ref sent.
	callsBefore := fa.calls
	cl2, err := s.claim(ctx, uid, pid)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if n := len(cl2["totals"].([]map[string]any)); n != 0 {
		t.Fatalf("double-claim paid out %d assets, want 0", n)
	}
	if fa.calls != callsBefore {
		t.Fatalf("double-claim issued %d new asset calls", fa.calls-callsBefore)
	}

	// All claimed_at timestamps are set.
	var unclaimed int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM s_rewards WHERE position_id=$1 AND claimed_at IS NULL`, pid).Scan(&unclaimed)
	if unclaimed != 0 {
		t.Fatalf("expected 0 unclaimed reward rows, got %d", unclaimed)
	}
}

func TestUnbondingReleasesPrincipalAndRewards(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	seedProduct(t, s, "test-unbond", "ATOM", 0.365, 2)

	fa.credit(uid, "ATOM", decimal.RequireFromString("50"))
	out, err := s.stake(ctx, uid, stakeReq{Asset: "ATOM", Validator: "test-unbond", Amount: "5"})
	if err != nil {
		t.Fatalf("stake: %v", err)
	}
	pid := out["positionId"].(int64)

	// Earn one day.
	advance(t, s, 25)

	// Unstake: status unbonding, principal still locked, unbond_at in future.
	uout, err := s.unstake(ctx, uid, unstakeReq{PositionID: pid})
	if err != nil {
		t.Fatalf("unstake: %v", err)
	}
	if uout["status"] != "unbonding" {
		t.Fatalf("unstake status = %v, want unbonding", uout["status"])
	}
	// Spot unchanged until release.
	if !fa.balance(uid, "ATOM").Equal(decimal.RequireFromString("45")) {
		t.Fatalf("spot before release = %s, want 45", fa.balance(uid, "ATOM"))
	}
	p := loadPosition(t, s, pid)
	if p.Status != "unbonding" || p.UnbondAt == nil {
		t.Fatalf("position after unstake: %+v", p)
	}

	// Jump past unbond_at and tick: principal + rewards credited to spot.
	advance(t, s, 48)
	// Explicit tick to run the release pass if advance already ticked past the moment.
	s.Tick(ctx)
	p = loadPosition(t, s, pid)
	if p.Status != "released" || p.ReleasedAt == nil {
		t.Fatalf("expected released after unbond window, got %+v", p)
	}
	// Spot gets at least the principal back plus ~1d of rewards earned before unstake.
	got := fa.balance(uid, "ATOM")
	want := decimal.RequireFromString("50.005")
	if got.LessThan(want) {
		t.Fatalf("spot after release = %s, want >= %s (principal 50 + rewards)", got, want)
	}

	// Partial unstake also works: start another position and leave only part unbonding.
	uid2 := testUser()
	fa.credit(uid2, "ATOM", decimal.RequireFromString("30"))
	out2, err := s.stake(ctx, uid2, stakeReq{Asset: "ATOM", Validator: "test-unbond", Amount: "20"})
	if err != nil {
		t.Fatalf("stake2: %v", err)
	}
	pid2 := out2["positionId"].(int64)
	if _, err := s.unstake(ctx, uid2, unstakeReq{PositionID: pid2, Amount: "7"}); err != nil {
		t.Fatalf("partial unstake: %v", err)
	}
	// Original position shrunk to 13 and stays active; new row for the leaving 7 is unbonding.
	original := loadPosition(t, s, pid2)
	if original.Status != "active" || !original.Amount.Equal(decimal.RequireFromString("13")) {
		t.Fatalf("remainder after partial = %+v", original)
	}
	var leavingAmount decimal.Decimal
	var leavingStatus string
	err = s.db.QueryRowContext(ctx, `SELECT amount, status FROM s_positions WHERE uid=$1 AND product_id='test-unbond' AND id<>$2 ORDER BY id DESC LIMIT 1`, uid2, pid2).
		Scan(&leavingAmount, &leavingStatus)
	if err != nil {
		t.Fatalf("find leaving row: %v", err)
	}
	if leavingStatus != "unbonding" || !leavingAmount.Equal(decimal.RequireFromString("7")) {
		t.Fatalf("leaving row amount=%s status=%s, want 7/unbonding", leavingAmount, leavingStatus)
	}
}

func TestInstantUnstakeForZeroUnbonding(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	seedProduct(t, s, "test-instant", "ADA", 0.1, 0)

	fa.credit(uid, "ADA", decimal.RequireFromString("1000"))
	out, _ := s.stake(ctx, uid, stakeReq{Asset: "ADA", Validator: "test-instant", Amount: "500"})
	pid := out["positionId"].(int64)
	uo, err := s.unstake(ctx, uid, unstakeReq{PositionID: pid})
	if err != nil {
		t.Fatalf("unstake: %v", err)
	}
	if uo["status"] != "released" {
		t.Fatalf("instant unstake status=%v, want released", uo["status"])
	}
	if !fa.balance(uid, "ADA").Equal(decimal.RequireFromString("1000")) {
		t.Fatalf("spot after instant release = %s, want 1000", fa.balance(uid, "ADA"))
	}
}

func TestResolveProductByIdOrName(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	uid := testUser()
	seedProduct(t, s, "test-resolve", "DOT", 0.1, 1)
	// Rename validator so both the id path and the (asset, validator) path exist.
	_, err := s.db.ExecContext(ctx, `UPDATE s_products SET validator='Polkadot Alpha' WHERE id='test-resolve'`)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	fa.credit(uid, "DOT", decimal.RequireFromString("100"))

	// By id.
	if _, err := s.stake(ctx, uid, stakeReq{Asset: "DOT", Validator: "test-resolve", Amount: "10"}); err != nil {
		t.Fatalf("stake by id: %v", err)
	}
	// By validator display name (case-insensitive).
	if _, err := s.stake(ctx, uid, stakeReq{Asset: "DOT", Validator: "polkadot alpha", Amount: "10"}); err != nil {
		t.Fatalf("stake by validator name: %v", err)
	}
	// Mismatched asset is rejected.
	if _, err := s.stake(ctx, uid, stakeReq{Asset: "BTC", Validator: "test-resolve", Amount: "10"}); err == nil {
		t.Fatal("stake with mismatched asset should fail")
	}
}

func TestUnstakeOnlyOwner(t *testing.T) {
	s, fa := newTestServer(t)
	ctx := context.Background()
	alice, bob := testUser(), testUser()
	seedProduct(t, s, "test-owner", "SOL", 0.1, 1)
	fa.credit(alice, "SOL", decimal.RequireFromString("10"))

	out, err := s.stake(ctx, alice, stakeReq{Asset: "SOL", Validator: "test-owner", Amount: "5"})
	if err != nil {
		t.Fatalf("stake: %v", err)
	}
	pid := out["positionId"].(int64)
	if _, err := s.unstake(ctx, bob, unstakeReq{PositionID: pid}); err == nil {
		t.Fatal("unstake by non-owner should fail")
	}
	if _, err := s.claim(ctx, bob, pid); err == nil {
		t.Fatal("claim by non-owner should fail")
	}
}

func TestDailyReward(t *testing.T) {
	cases := []struct {
		name          string
		principal     string
		apr           string
		wantPerDay    string
	}{
		{"zero principal", "0", "0.1", "0"},
		{"zero apr", "100", "0", "0"},
		{"small", "1", "0.365", "0.001"},
		{"rounds to 8dp floor", "1", "0.07305", "0.00020013"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dailyReward(decimal.RequireFromString(c.principal), decimal.RequireFromString(c.apr))
			if !got.Equal(decimal.RequireFromString(c.wantPerDay)) {
				t.Fatalf("got %s want %s", got, c.wantPerDay)
			}
		})
	}
}

func TestHandlerValidation(t *testing.T) {
	s, _ := newTestServer(t)

	// Unauthorized on /staking/stake without X-User-Id.
	req := httptest.NewRequest("POST", "/staking/stake", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status=%d, want 401", w.Code)
	}

	// Public catalogue requires no auth.
	req = httptest.NewRequest("GET", "/staking/validators", nil)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validators: status=%d, want 200", w.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode validators: %v", err)
	}
	if len(body.Items) == 0 {
		t.Fatal("expected seeded products, got none")
	}
}
