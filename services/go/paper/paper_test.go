package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

type fakeMarket struct {
	symbol  Symbol
	quote   Quote
	err     error
	indexes atomic.Int64
}

func (m *fakeMarket) TradingSymbol(context.Context, string) (Symbol, error) { return m.symbol, m.err }
func (m *fakeMarket) Symbol(context.Context, string) (Symbol, error)        { return m.symbol, m.err }
func (m *fakeMarket) Index(context.Context, string) (Quote, error) {
	m.indexes.Add(1)
	return m.quote, m.err
}
func testMarket(px string) *fakeMarket {
	return &fakeMarket{symbol: Symbol{Symbol: "BTCUSDT", Base: "BTC", Quote: "USDT", PricePrecision: 2, QuantityPrecision: 6, MinQuantity: decimal.RequireFromString("0.000001"), MinNotional: decimal.RequireFromString("5"), Status: "trading"}, quote: Quote{Symbol: "BTCUSDT", Price: decimal.RequireFromString(px), Source: "test-external", ObservedAt: time.Now().UTC()}}
}

func apiCode(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; run through a local temporary PostgreSQL")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("paper_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`) })
	url, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	url.Path = "/" + name
	db, err := sql.Open("pgx", url.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(32)
	return db
}
func migratedStore(t *testing.T) *Store {
	t.Helper()
	db := testDB(t)
	ctx := context.Background()
	for _, query := range []string{
		`CREATE TABLE symbols (symbol TEXT PRIMARY KEY,base TEXT,quote TEXT,price_precision INT,quantity_precision INT,min_quantity NUMERIC,min_notional NUMERIC,status TEXT)`,
		`CREATE TABLE balances (user_id TEXT,asset TEXT,free NUMERIC,locked NUMERIC)`,
		`CREATE TABLE ledger_transactions (id BIGSERIAL PRIMARY KEY)`,
		`CREATE TABLE ledger_entries (id BIGSERIAL PRIMARY KEY)`,
		`CREATE TABLE orders (id BIGSERIAL PRIMARY KEY)`,
		`CREATE TABLE trades (id BIGSERIAL PRIMARY KEY)`,
		`CREATE TABLE app_features (key TEXT PRIMARY KEY,enabled BOOLEAN)`,
		`INSERT INTO balances VALUES ('80000001','USDT',12345,0),('80000001','BTC',7,0)`,
		`INSERT INTO app_features VALUES ('paperTrading',TRUE),('priceAlerts',TRUE)`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return NewStore(db)
}
func TestPaperIsolationReplayAndReset(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	user := "80000001"
	m := testMarket("50000.00")
	input := PlacePaperOrder{Symbol: "BTCUSDT", Side: "buy", Type: "market", Quantity: "0.100000", ClientOrderID: "paper-order-1"}
	first, err := store.PlaceOrder(ctx, user, input, m)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.PlaceOrder(ctx, user, input, m)
	if err != nil {
		t.Fatal(err)
	}
	if first.OrderID != replay.OrderID || first.ReferencePrice != replay.ReferencePrice || !first.CreatedAt.Equal(replay.CreatedAt) {
		t.Fatalf("stored response changed: %#v %#v", first, replay)
	}
	var rawOriginal json.RawMessage
	if err := store.db.QueryRow(`SELECT response_json FROM paper_orders WHERE order_id=$1`, first.OrderID).Scan(&rawOriginal); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(first, rawOriginal) {
		t.Fatalf("db response differs: %s", rawOriginal)
	}
	oldGeneration, err := store.ListOrders(ctx, user, "", "filled", 1, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldGeneration["items"].([]PaperOrder)) != 1 {
		t.Fatalf("first generation hidden: %#v", oldGeneration)
	}
	if first.OrderID != replay.OrderID || m.indexes.Load() != 1 {
		t.Fatalf("idempotency used fresh execution: %v %v calls=%d", first.OrderID, replay.OrderID, m.indexes.Load())
	}
	var realUSDT, realBTC string
	if err := store.db.QueryRow(`SELECT free::text FROM balances WHERE user_id=$1 AND asset='USDT'`, user).Scan(&realUSDT); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT free::text FROM balances WHERE user_id=$1 AND asset='BTC'`, user).Scan(&realBTC); err != nil {
		t.Fatal(err)
	}
	if realUSDT != "12345" || realBTC != "7" {
		t.Fatalf("real balance changed: %s %s", realUSDT, realBTC)
	}
	var paperUSDT, paperBTC string
	if err := store.db.QueryRow(`SELECT free::text FROM paper_balances p JOIN paper_accounts a ON a.id=p.account_id WHERE a.user_id=$1 AND p.asset='USDT'`, user).Scan(&paperUSDT); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT free::text FROM paper_balances p JOIN paper_accounts a ON a.id=p.account_id WHERE a.user_id=$1 AND p.asset='BTC'`, user).Scan(&paperBTC); err != nil {
		t.Fatal(err)
	}
	if decimal.RequireFromString(paperUSDT).String() != "5000" || decimal.RequireFromString(paperBTC).String() != "0.1" {
		t.Fatalf("paper balance wrong: %s %s", paperUSDT, paperBTC)
	}
	for _, table := range []string{"ledger_transactions", "ledger_entries", "orders", "trades"} {
		var n int
		if err := store.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("production %s count=%d err=%v", table, n, err)
		}
	}
	if _, err := store.PlaceOrder(ctx, user, PlacePaperOrder{Symbol: "BTCUSDT", Side: "buy", Type: "market", Quantity: "0.200000", ClientOrderID: "paper-order-1"}, m); apiCode(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("idempotency mismatch err=%v", err)
	}
	reset, err := store.Reset(ctx, user, "reset-key-1", "RESET_PAPER_ACCOUNT")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := store.Reset(ctx, user, "reset-key-1", "RESET_PAPER_ACCOUNT")
	if err != nil {
		t.Fatal(err)
	}
	if reset.Generation != repeat.Generation || reset.ResetAt != repeat.ResetAt {
		t.Fatalf("reset replay not exact: %#v %#v", reset, repeat)
	}
	var orders int
	if err := store.db.QueryRow(`SELECT count(*) FROM paper_orders WHERE generation=$1`, reset.Generation).Scan(&orders); err != nil || orders != 0 {
		t.Fatalf("old generation visible after reset: %d %v", orders, err)
	}
	var remaining int
	if err := store.db.QueryRow(`SELECT count(*) FROM paper_orders`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("audit history not retained: %d %v", remaining, err)
	}
	list, err := store.ListOrders(ctx, user, "", "filled", 0, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list["items"].([]PaperOrder)) != 0 || list["generation"] != reset.Generation {
		t.Fatalf("default generation: %#v", list)
	}
	archived, err := store.ListOrders(ctx, user, "", "filled", 1, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived["items"].([]PaperOrder)) != 1 {
		t.Fatalf("explicit archive: %#v", archived)
	}
	if _, err := store.Reset(ctx, user, "reset-key-2", "RESET_PAPER_ACCOUNT"); apiCode(err) != "PAPER_RESET_LIMIT_EXCEEDED" {
		t.Fatalf("UTC limit missing: %v", err)
	}
}

func TestPaperRejectsStaleOrUnavailableIndex(t *testing.T) {
	store := migratedStore(t)
	m := testMarket("50000.00")
	m.err = apiErr(http.StatusServiceUnavailable, "PRICE_STALE", "stale")
	_, err := store.PlaceOrder(context.Background(), "80000001", PlacePaperOrder{Symbol: "BTCUSDT", Side: "buy", Type: "market", Quantity: "0.100000", ClientOrderID: "stale-reject"}, m)
	if apiCode(err) != "PRICE_STALE" {
		t.Fatalf("err=%v", err)
	}
	var orders int
	if err := store.db.QueryRow(`SELECT count(*) FROM paper_orders`).Scan(&orders); err != nil || orders != 0 {
		t.Fatalf("stale fill persisted: %d %v", orders, err)
	}
	var balance string
	if err := store.db.QueryRow(`SELECT free::text FROM balances WHERE user_id='80000001' AND asset='USDT'`).Scan(&balance); err != nil || balance != "12345" {
		t.Fatalf("stale quote changed real balance: %s %v", balance, err)
	}
}

func jsonEqual(v any, raw []byte) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var a, c any
	return json.Unmarshal(b, &a) == nil && json.Unmarshal(raw, &c) == nil && reflect.DeepEqual(a, c)
}
