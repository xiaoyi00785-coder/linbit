package main

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// Regression: AmendAlgo used to take s.mu and then call withTx, which takes
// s.mu again -> self-deadlock. The DB here is unreachable, so the call must
// return an error promptly instead of hanging. Also checks s.mu is released.
func TestAmendAlgoNoDeadlock(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://x:x@127.0.0.1:1/x?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	for _, kinds := range [][]string{{"plan"}, {"tp", "sl"}} {
		done := make(chan error, 1)
		go func() {
			_, err := s.AmendAlgo(context.Background(), "u:a", 1, kinds, AlgoAmendReq{TriggerPrice: "100"})
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("%v: expected DB error", kinds)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: AmendAlgo deadlocked", kinds)
		}
		locked := make(chan struct{})
		go func() { s.mu.Lock(); s.mu.Unlock(); close(locked) }()
		select {
		case <-locked:
		case <-time.After(time.Second):
			t.Fatalf("%v: s.mu left locked", kinds)
		}
	}
}
