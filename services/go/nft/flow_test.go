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
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

// fakeAsset simulates the asset service's /internal/user-transfer with an
// in-memory ledger. It records every transfer so tests can assert on the
// seller/royalty split.
type fakeAsset struct {
	mu       sync.Mutex
	balances map[string]map[string]decimal.Decimal
	calls    []assetCall
	refs     map[string]bool
	fail     map[string]bool // simulate INSUFFICIENT_BALANCE on specific refs
}

type assetCall struct {
	From, To, Asset, Ref string
	Amount               decimal.Decimal
}

func newFakeAsset() *fakeAsset {
	return &fakeAsset{balances: map[string]map[string]decimal.Decimal{}, refs: map[string]bool{}, fail: map[string]bool{}}
}

func (f *fakeAsset) credit(uid, asset string, amount decimal.Decimal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.balances[uid] == nil {
		f.balances[uid] = map[string]decimal.Decimal{}
	}
	f.balances[uid][asset] = f.balances[uid][asset].Add(amount)
}

func (f *fakeAsset) balance(uid, asset string) decimal.Decimal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.balances[uid][asset]
}

func (f *fakeAsset) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/user-transfer", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != token {
			http.Error(w, `{"code":"AUTH_UNAUTHORIZED","message":"bad token"}`, http.StatusUnauthorized)
			return
		}
		var req struct {
			UserID, ToUser, Asset, Amount, Ref, Account string
		}
		b, _ := io.ReadAll(r.Body)
		raw := map[string]string{}
		_ = json.Unmarshal(b, &raw)
		req.UserID, req.ToUser, req.Asset, req.Amount, req.Ref, req.Account =
			raw["userId"], raw["toUserId"], raw["asset"], raw["amount"], raw["ref"], raw["account"]
		amount, err := decimal.NewFromString(req.Amount)
		if err != nil || amount.Sign() <= 0 {
			http.Error(w, `{"code":"VALIDATION_ERROR","message":"bad"}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail[req.Ref] {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"INSUFFICIENT_BALANCE","message":"low"}`))
			return
		}
		if f.refs[req.Ref] {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"already_applied"}`))
			return
		}
		cur := f.balances[req.UserID][req.Asset]
		if cur.LessThan(amount) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"INSUFFICIENT_BALANCE","message":"low"}`))
			return
		}
		if f.balances[req.UserID] == nil {
			f.balances[req.UserID] = map[string]decimal.Decimal{}
		}
		if f.balances[req.ToUser] == nil {
			f.balances[req.ToUser] = map[string]decimal.Decimal{}
		}
		f.balances[req.UserID][req.Asset] = cur.Sub(amount)
		f.balances[req.ToUser][req.Asset] = f.balances[req.ToUser][req.Asset].Add(amount)
		f.refs[req.Ref] = true
		f.calls = append(f.calls, assetCall{From: req.UserID, To: req.ToUser, Asset: req.Asset, Amount: amount, Ref: req.Ref})
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

type testEnv struct {
	t      *testing.T
	srv    *Server
	assets *fakeAsset
	asset  *httptest.Server
	db     *sql.DB
}

var testSeq atomic.Int64

func testUID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), testSeq.Add(1))
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DATABASE_URL set but unreachable (%v); skipping", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fa := newFakeAsset()
	assetSrv := httptest.NewServer(fa.handler("itkn"))
	t.Cleanup(assetSrv.Close)
	cfg := Config{
		Port: "0", DatabaseURL: dsn, InternalToken: "itkn", DevMode: true,
		AssetURL: assetSrv.URL, ExpiryEvery: time.Hour,
		PayAsset: "USDT", ListingMaxDur: 365 * 24 * time.Hour, OfferMaxDur: 60 * 24 * time.Hour,
	}
	return &testEnv{t: t, srv: NewServer(cfg, db), assets: fa, asset: assetSrv, db: db}
}

func (e *testEnv) do(method, path, uid, body string) (*http.Response, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, r)
	if uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.srv.Routes().ServeHTTP(rec, req)
	resp := rec.Result()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (e *testEnv) internal(method, path, body string) (*http.Response, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("X-Internal-Token", "itkn")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.srv.Routes().ServeHTTP(rec, req)
	resp := rec.Result()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func mustJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json: %v (body=%s)", err, string(b))
	}
	return out
}

func mustOK(t *testing.T, resp *http.Response, b []byte) map[string]any {
	t.Helper()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("status %d: %s", resp.StatusCode, string(b))
	}
	return mustJSON(t, b)
}

func TestFullFlow_ListBuyRoyalty(t *testing.T) {
	e := newTestEnv(t)
	creator := testUID("creator")
	seller := testUID("seller")
	buyer := testUID("buyer")

	// Fund buyer so settlement succeeds.
	e.assets.credit(buyer, "USDT", decimal.NewFromInt(1000))

	// Admin creates a collection with a 5% royalty.
	slug := "coll-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	res, b := e.internal("POST", "/internal/nft/collections", fmt.Sprintf(
		`{"slug":%q,"name":"Test","creatorId":%q,"royaltyBps":500,"payAsset":"USDT"}`, slug, creator))
	col := mustOK(t, res, b)
	if col["slug"].(string) != slug {
		t.Fatalf("slug mismatch: %v", col)
	}

	// Mint one item to the seller.
	res, b = e.internal("POST", "/internal/nft/items", fmt.Sprintf(
		`{"collectionSlug":%q,"ownerId":%q,"name":"Linbird #1"}`, slug, seller))
	it := mustOK(t, res, b)
	itemID := int64(it["id"].(float64))

	// Seller lists at 100 USDT.
	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/listings", itemID), seller,
		`{"price":"100","durationDays":7}`)
	l := mustOK(t, res, b)
	listingID := int64(l["id"].(float64))
	if l["price"].(string) != "100" {
		t.Fatalf("list price: %v", l["price"])
	}

	// Buyer buys with maxPrice check.
	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/buy", itemID), buyer,
		`{"maxPrice":"200"}`)
	trade := mustOK(t, res, b)
	if sa := trade["sellerAmount"].(string); sa != "95" {
		t.Fatalf("sellerAmount = %q, want 95", sa)
	}
	if ra := trade["royaltyAmount"].(string); ra != "5" {
		t.Fatalf("royaltyAmount = %q, want 5", ra)
	}

	// Ledger: buyer 1000 → seller 95, creator 5, buyer 900.
	if got := e.assets.balance(seller, "USDT"); !got.Equal(decimal.NewFromInt(95)) {
		t.Fatalf("seller balance = %s", got)
	}
	if got := e.assets.balance(creator, "USDT"); !got.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("creator balance = %s", got)
	}
	if got := e.assets.balance(buyer, "USDT"); !got.Equal(decimal.NewFromInt(900)) {
		t.Fatalf("buyer balance = %s", got)
	}

	// Ownership moved, listing closed.
	res, b = e.do("GET", fmt.Sprintf("/nft/items/%d", itemID), buyer, "")
	d := mustOK(t, res, b)
	item := d["item"].(map[string]any)
	if item["ownerId"].(string) != buyer {
		t.Fatalf("owner = %v", item["ownerId"])
	}
	if item["listed"].(bool) {
		t.Fatalf("still listed after sale")
	}
	if _, ok := d["listing"]; ok {
		t.Fatalf("listing still present after sale")
	}

	// Activity: should have mint, list, sale, transfer in reverse-id order.
	res, b = e.do("GET", fmt.Sprintf("/nft/items/%d/activity", itemID), "", "")
	acts := mustOK(t, res, b)["items"].([]any)
	kinds := []string{}
	for _, a := range acts {
		kinds = append(kinds, a.(map[string]any)["kind"].(string))
	}
	want := map[string]bool{"mint": false, "list": false, "sale": false, "transfer": false}
	for _, k := range kinds {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("activity missing %q (got %v)", k, kinds)
		}
	}

	// Favorite toggle.
	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/favorite", itemID), buyer, "")
	if fav := mustOK(t, res, b)["favorited"].(bool); !fav {
		t.Fatalf("expected favorited=true, got %v", fav)
	}
	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/favorite", itemID), buyer, "")
	if fav := mustOK(t, res, b)["favorited"].(bool); fav {
		t.Fatalf("expected favorited=false after second toggle")
	}

	// Avoid used-only-in-test warnings.
	_ = listingID
	_ = context.Background()
}

func TestOfferFlow_AcceptAndExpire(t *testing.T) {
	e := newTestEnv(t)
	creator := testUID("creator")
	seller := testUID("seller")
	bidder := testUID("bidder")
	other := testUID("other") // offers from this user should expire

	e.assets.credit(bidder, "USDT", decimal.NewFromInt(500))
	e.assets.credit(other, "USDT", decimal.NewFromInt(500))

	slug := "off-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	e.internal("POST", "/internal/nft/collections", fmt.Sprintf(
		`{"slug":%q,"name":"Off","creatorId":%q,"royaltyBps":1000,"payAsset":"USDT"}`, slug, creator))
	res, b := e.internal("POST", "/internal/nft/items", fmt.Sprintf(
		`{"collectionSlug":%q,"ownerId":%q,"name":"B #1"}`, slug, seller))
	itemID := int64(mustOK(t, res, b)["id"].(float64))

	// Bidder makes an offer; another user makes an offer that will expire.
	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/offers", itemID), bidder,
		`{"price":"50","durationDays":7}`)
	off := mustOK(t, res, b)
	offerID := int64(off["id"].(float64))

	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/offers", itemID), other,
		`{"price":"10","durationDays":1}`)
	shortOff := mustOK(t, res, b)
	shortOfferID := int64(shortOff["id"].(float64))

	// Seller accepts the top offer.
	res, b = e.do("POST", fmt.Sprintf("/nft/offers/%d/accept", offerID), seller, "")
	ac := mustOK(t, res, b)
	if sa := ac["sellerAmount"].(string); sa != "45" {
		t.Fatalf("sellerAmount = %s, want 45", sa)
	}
	if ra := ac["royaltyAmount"].(string); ra != "5" {
		t.Fatalf("royaltyAmount = %s, want 5", ra)
	}

	if got := e.assets.balance(seller, "USDT"); !got.Equal(decimal.NewFromInt(45)) {
		t.Fatalf("seller = %s", got)
	}
	if got := e.assets.balance(creator, "USDT"); !got.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("creator = %s", got)
	}

	// Short offer should still be active, then advance the clock and run tick.
	res, b = e.internal("POST", "/internal/dev/nft/advance", `{"hours":48}`)
	mustOK(t, res, b)

	// Query — the short offer must now be expired.
	var status string
	if err := e.db.QueryRow(`SELECT status FROM nft_offers WHERE id=$1`, shortOfferID).Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "expired" {
		t.Fatalf("other offer status = %s, want expired", status)
	}

	// Expiry activity written for the offer.
	rows, err := e.db.Query(`SELECT kind FROM nft_activity WHERE item_id=$1 ORDER BY id`, itemID)
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	defer rows.Close()
	saw := map[string]bool{}
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		saw[k] = true
	}
	for _, k := range []string{"mint", "offer", "sale", "transfer", "offer_expired"} {
		if !saw[k] {
			t.Fatalf("missing activity %q (saw %v)", k, saw)
		}
	}
}

func TestListingExpiryWorker(t *testing.T) {
	e := newTestEnv(t)
	seller := testUID("sx")
	slug := "exp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	e.internal("POST", "/internal/nft/collections", fmt.Sprintf(
		`{"slug":%q,"name":"Exp","creatorId":%q,"royaltyBps":0,"payAsset":"USDT"}`, slug, testUID("c")))
	res, b := e.internal("POST", "/internal/nft/items", fmt.Sprintf(
		`{"collectionSlug":%q,"ownerId":%q,"name":"X"}`, slug, seller))
	itemID := int64(mustOK(t, res, b)["id"].(float64))

	e.do("POST", fmt.Sprintf("/nft/items/%d/listings", itemID), seller,
		`{"price":"20","durationDays":1}`)

	// Advance 2 days, run tick.
	e.internal("POST", "/internal/dev/nft/advance", `{"hours":48}`)

	var listed bool
	var price sql.NullString
	if err := e.db.QueryRow(`SELECT listed, list_price::text FROM nft_items WHERE id=$1`, itemID).Scan(&listed, &price); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if listed || price.Valid {
		t.Fatalf("item still listed after expiry (listed=%v price=%v)", listed, price)
	}

	var status string
	if err := e.db.QueryRow(`SELECT status FROM nft_listings WHERE item_id=$1`, itemID).Scan(&status); err != nil {
		t.Fatalf("scan listing: %v", err)
	}
	if status != "expired" {
		t.Fatalf("listing status = %s, want expired", status)
	}
}

func TestBuy_InsufficientBalanceRollsBack(t *testing.T) {
	e := newTestEnv(t)
	seller := testUID("s")
	buyer := testUID("b")
	slug := "ins-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	e.internal("POST", "/internal/nft/collections", fmt.Sprintf(
		`{"slug":%q,"name":"Ins","creatorId":%q,"royaltyBps":0,"payAsset":"USDT"}`, slug, testUID("c")))
	res, b := e.internal("POST", "/internal/nft/items", fmt.Sprintf(
		`{"collectionSlug":%q,"ownerId":%q,"name":"Y"}`, slug, seller))
	itemID := int64(mustOK(t, res, b)["id"].(float64))
	e.do("POST", fmt.Sprintf("/nft/items/%d/listings", itemID), seller, `{"price":"100"}`)

	// Buyer has no balance → asset service returns INSUFFICIENT_BALANCE.
	res, b = e.do("POST", fmt.Sprintf("/nft/items/%d/buy", itemID), buyer, `{}`)
	if resp := mustJSON(t, b); resp["code"] != "INSUFFICIENT_BALANCE" {
		t.Fatalf("code = %v, body=%s (status=%d)", resp["code"], string(b), res.StatusCode)
	}

	// Ownership and listing must be unchanged.
	var owner string
	var listed bool
	if err := e.db.QueryRow(`SELECT owner_id, listed FROM nft_items WHERE id=$1`, itemID).Scan(&owner, &listed); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if owner != seller || !listed {
		t.Fatalf("after failed buy: owner=%s listed=%v, want %s true", owner, listed, seller)
	}
	var lstatus string
	if err := e.db.QueryRow(`SELECT status FROM nft_listings WHERE item_id=$1`, itemID).Scan(&lstatus); err != nil {
		t.Fatalf("scan listing: %v", err)
	}
	if lstatus != "active" {
		t.Fatalf("listing status = %s, want active", lstatus)
	}

	// No trade was recorded.
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM nft_trades WHERE item_id=$1`, itemID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 trades after failed buy, got %d", n)
	}
}

func TestCollectionAggregatesAfterSale(t *testing.T) {
	e := newTestEnv(t)
	creator := testUID("cx")
	s1, s2 := testUID("s1"), testUID("s2")
	buyer := testUID("by")
	e.assets.credit(buyer, "USDT", decimal.NewFromInt(500))

	slug := "ag-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	e.internal("POST", "/internal/nft/collections", fmt.Sprintf(
		`{"slug":%q,"name":"Ag","creatorId":%q,"royaltyBps":250,"payAsset":"USDT"}`, slug, creator))
	res, b := e.internal("POST", "/internal/nft/items", fmt.Sprintf(
		`{"collectionSlug":%q,"ownerId":%q,"name":"A"}`, slug, s1))
	i1 := int64(mustOK(t, res, b)["id"].(float64))
	res, b = e.internal("POST", "/internal/nft/items", fmt.Sprintf(
		`{"collectionSlug":%q,"ownerId":%q,"name":"B"}`, slug, s2))
	_ = int64(mustOK(t, res, b)["id"].(float64))

	e.do("POST", fmt.Sprintf("/nft/items/%d/listings", i1), s1, `{"price":"80"}`)
	e.do("POST", fmt.Sprintf("/nft/items/%d/buy", i1), buyer, `{}`)

	// After sale, recomputed collection row should reflect one sale + volume.
	res, b = e.do("GET", "/nft/collections/"+slug, "", "")
	col := mustOK(t, res, b)
	if col["salesTotal"].(float64) != 1 {
		t.Fatalf("salesTotal = %v want 1", col["salesTotal"])
	}
	if col["volumeTotal"].(string) != "80" {
		t.Fatalf("volumeTotal = %v want 80", col["volumeTotal"])
	}
	// 24h volume refresh happens in Tick — kick one.
	e.srv.Tick(context.Background())
	res, b = e.do("GET", "/nft/collections/"+slug, "", "")
	col = mustOK(t, res, b)
	if col["volume24h"].(string) != "80" {
		t.Fatalf("volume24h = %v want 80", col["volume24h"])
	}
}
