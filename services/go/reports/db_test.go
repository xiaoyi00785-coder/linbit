package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// bootstrapSchemas creates the subset of the upstream services' tables that
// the reports service reads from. In production those tables are owned by the
// asset / wallet / order services and migrated at their startup. For tests we
// apply them here so the fetch queries resolve.
const bootstrapSchemas = `
CREATE TABLE IF NOT EXISTS trades (
    id BIGSERIAL PRIMARY KEY,
    trade_id TEXT NOT NULL, order_id TEXT NOT NULL, user_id TEXT NOT NULL,
    symbol TEXT NOT NULL, side TEXT NOT NULL,
    price NUMERIC(36,18) NOT NULL, quantity NUMERIC(36,18) NOT NULL,
    fee NUMERIC(36,18) NOT NULL DEFAULT 0, fee_asset TEXT, role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deposits (
    id BIGSERIAL PRIMARY KEY, user_id TEXT NOT NULL,
    asset TEXT NOT NULL, network TEXT NOT NULL, address TEXT NOT NULL,
    amount NUMERIC(36,18) NOT NULL CHECK (amount > 0),
    tx_hash TEXT NOT NULL, log_index BIGINT NOT NULL DEFAULT 0,
    confirmations BIGINT NOT NULL DEFAULT 0, status TEXT NOT NULL,
    credited_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS withdrawals (
    id BIGSERIAL PRIMARY KEY, user_id TEXT NOT NULL,
    asset TEXT NOT NULL, network TEXT NOT NULL, address TEXT NOT NULL,
    amount NUMERIC(36,18) NOT NULL, gas_fee NUMERIC(36,18) NOT NULL DEFAULT 0,
    gas_fee_asset TEXT NOT NULL, status TEXT NOT NULL, tx_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS internal_transfers (
    id BIGSERIAL PRIMARY KEY, user_id TEXT NOT NULL, to_user_id TEXT NOT NULL,
    client_id TEXT NOT NULL, asset TEXT NOT NULL,
    amount NUMERIC(36,18) NOT NULL, value_usdt NUMERIC(36,18) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending', created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS funding_balances (
    user_id TEXT NOT NULL, asset TEXT NOT NULL,
    available NUMERIC(36,18) NOT NULL DEFAULT 0, frozen NUMERIC(36,18) NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, asset)
);

CREATE TABLE IF NOT EXISTS ledger_transactions (
    id BIGSERIAL PRIMARY KEY, user_id TEXT NOT NULL,
    tx_type TEXT NOT NULL, ref TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ledger_entries (
    id BIGSERIAL PRIMARY KEY, tx_id BIGINT NOT NULL REFERENCES ledger_transactions(id) ON DELETE CASCADE,
    account TEXT NOT NULL, asset TEXT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('debit','credit')),
    amount NUMERIC(36,18) NOT NULL CHECK (amount > 0)
);`

// fakeAccount captures POST /internal/notifications so we can assert that
// subscription fan-out reached the account service.
type fakeAccount struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []map[string]any
}

func newFakeAccount(t *testing.T) *fakeAccount {
	fa := &fakeAccount{}
	fa.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/internal/notifications") {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fa.mu.Lock()
		fa.hits = append(fa.hits, body)
		fa.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fa.srv.Close)
	return fa
}

func (fa *fakeAccount) count() int {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	return len(fa.hits)
}

func newTestServer(t *testing.T) *Server {
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
	if _, err := db.Exec(bootstrapSchemas); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := Config{
		Port:              "0",
		DatabaseURL:       dsn,
		JWTSecret:         "test-secret",
		InternalToken:     "test-token",
		DevMode:           true,
		ExportsDir:        t.TempDir(),
		ExportsTTL:        time.Hour,
		DownloadBaseURL:   "/reports/exports",
		WatcherTick:       50 * time.Millisecond,
		MaxExportRows:     1000,
		TaxCountries:      []string{"US", "DE", "JP", "CN"},
		DefaultTaxCountry: "US",
	}
	return NewServer(cfg, db)
}

// testUser returns a fresh uid per test, scoped to the test name so parallel
// runs do not share rows.
func testUser(t *testing.T) string {
	t.Helper()
	return "u-" + strings.ReplaceAll(t.Name(), "/", "-")
}

// cleanupUser wipes every reports-owned row for the uid and the upstream
// bootstrap tables so repeated runs stay deterministic.
func cleanupUser(t *testing.T, s *Server, uid string) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		"DELETE FROM report_exports WHERE user_id=$1",
		"DELETE FROM notification_subscriptions WHERE user_id=$1",
		"DELETE FROM trades WHERE user_id=$1",
		"DELETE FROM deposits WHERE user_id=$1",
		"DELETE FROM withdrawals WHERE user_id=$1",
		"DELETE FROM internal_transfers WHERE user_id=$1 OR to_user_id=$1",
		"DELETE FROM funding_balances WHERE user_id=$1",
		`DELETE FROM ledger_entries WHERE tx_id IN (SELECT id FROM ledger_transactions WHERE user_id=$1)`,
		"DELETE FROM ledger_transactions WHERE user_id=$1",
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q, uid); err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}
	// Reset watermarks so the watcher sees newly inserted rows in the next tick.
	_, _ = s.db.ExecContext(ctx, `UPDATE report_cursors SET position=0`)
	// Also clear any leftover jobs from a crashed prior run so the queue is
	// deterministic for this test — LeaseNextExport does not filter by user.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM report_exports`)
}
