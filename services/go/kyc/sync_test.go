package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSyncBackoff(t *testing.T) {
	base, max := 5*time.Second, time.Minute
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := syncBackoff(i+1, base, max); got != w {
			t.Fatalf("attempt %d: got %s want %s", i+1, got, w)
		}
	}
}

func TestValidSideProofOfAddress(t *testing.T) {
	if !validSide("proof_of_address") {
		t.Fatal("proof_of_address must be a valid side")
	}
}

// TestLevelSyncRetries persists a failed sync, backs off, then succeeds once the
// account service recovers. DB-backed; skips without DATABASE_URL.
func TestLevelSyncRetries(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var up atomic.Bool
	var calls atomic.Int32
	acct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer acct.Close()
	cfg := Config{SyncBaseDelay: time.Second, SyncMaxDelay: time.Hour}
	store := NewStore(db)
	s := &Server{cfg: cfg, store: store, account: NewAccountClient(acct.URL, "t")}
	ctx := context.Background()
	uid := "synctest-" + time.Now().Format("150405.000000")

	if err := store.EnqueueLevelSync(ctx, uid, 2); err != nil {
		t.Fatal(err)
	}
	if s.trySync(ctx, uid, 2, 0) {
		t.Fatal("expected failure while account is down")
	}
	rows, _ := store.ListLevelSyncs(ctx, 1000)
	var row *LevelSync
	for i := range rows {
		if rows[i].UserID == uid {
			row = &rows[i]
		}
	}
	if row == nil || row.Attempts != 1 || row.LastError == nil || !row.NextAttemptAt.After(time.Now()) {
		t.Fatalf("failure not persisted with backoff: %+v", row)
	}
	// Not due yet: a pass must not call account for this user.
	before := calls.Load()
	db.Exec(`UPDATE kyc_level_sync SET next_attempt_at = now() + interval '1 hour' WHERE user_id <> $1`, uid)
	s.SyncDue(ctx)
	if calls.Load() != before {
		t.Fatal("retried before backoff elapsed")
	}
	// Recover and make it due.
	up.Store(true)
	db.Exec(`UPDATE kyc_level_sync SET next_attempt_at = now() WHERE user_id = $1`, uid)
	if s.SyncDue(ctx) < 1 {
		t.Fatal("expected sync to succeed after recovery")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM kyc_level_sync WHERE user_id = $1`, uid).Scan(&n)
	if n != 0 {
		t.Fatal("sync row not removed after success")
	}
}
