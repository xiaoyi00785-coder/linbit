package main

import (
	"context"
	"testing"
	"time"
)

func TestSkewTooLarge(t *testing.T) {
	if skewTooLarge(1_000, 0) {
		t.Fatal("missing watermark is not skew")
	}
	if skewTooLarge(3_000, 1_000) {
		t.Fatal("1s is inside the 2s limit")
	}
	if skewTooLarge(1_000, 3_000) {
		t.Fatal("engine ahead by 1s is inside the limit")
	}
	if !skewTooLarge(4_001, 1_000) {
		t.Fatal("3.001s must alert")
	}
	if !skewTooLarge(1_000, 4_001) {
		t.Fatal("engine ahead by more than 2s must alert")
	}
	if skewTooLarge(1_000+clockSkewLimit.Milliseconds(), 1_000) {
		t.Fatal("exactly the limit is not over it")
	}
}

func TestClassifyCancel(t *testing.T) {
	reason := "not_found"
	status, code, msg, stop := classifyCancel(&ExecReport{Status: "rejected", RejectReason: &reason})
	if !stop || status != 409 || code != "ORDER_SETTLING" || msg == "" {
		t.Fatalf("not_found: %d %s %s %v", status, code, msg, stop)
	}
	other := "duplicate"
	status, code, _, stop = classifyCancel(&ExecReport{Status: "rejected", RejectReason: &other})
	if !stop || code != "CANCEL_REJECTED" || status != 409 {
		t.Fatalf("other reject: %d %s %v", status, code, stop)
	}
	if _, _, _, stop = classifyCancel(&ExecReport{Status: "canceled"}); stop {
		t.Fatal("accepted cancel must not stop the handler")
	}
	if _, _, _, stop = classifyCancel(nil); stop {
		t.Fatal("nil report")
	}
}

func TestKafkaBufferContiguousStopsAtHole(t *testing.T) {
	k := &KafkaReportStream{buf: map[string]map[int64]StreamEntry{}}
	k.add("BTCUSDT", StreamEntry{Seq: 2})
	k.add("BTCUSDT", StreamEntry{Seq: 1})
	k.add("BTCUSDT", StreamEntry{Seq: 4})
	k.add("", StreamEntry{Seq: 9})
	got := k.contiguous("BTCUSDT", 0, 10)
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("prefix: %+v", got)
	}
	rest := k.contiguous("BTCUSDT", 3, 10)
	if len(rest) != 1 || rest[0].Seq != 4 {
		t.Fatalf("after hole: %+v", rest)
	}
}

func TestCursorLeaseExcludesOtherOwner(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sym := "LEASE" + testUser()
	if err := store.EnsureCursor(ctx, sym); err != nil {
		t.Fatal(err)
	}
	seq, ok, err := store.ClaimCursor(ctx, sym, "owner-a", time.Minute)
	if err != nil || !ok || seq != 0 {
		t.Fatalf("claim a: seq=%d ok=%v err=%v", seq, ok, err)
	}
	if _, ok, err = store.ClaimCursor(ctx, sym, "owner-b", time.Minute); err != nil || ok {
		t.Fatalf("owner b took a live lease: ok=%v err=%v", ok, err)
	}
	if _, ok, err = store.ClaimCursor(ctx, sym, "owner-a", time.Minute); err != nil || !ok {
		t.Fatalf("owner a renew: ok=%v err=%v", ok, err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE engine_cursors SET lease_until = now() - interval '1 second' WHERE symbol=$1`, sym); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = store.ClaimCursor(ctx, sym, "owner-b", time.Minute); err != nil || !ok {
		t.Fatalf("expired takeover: ok=%v err=%v", ok, err)
	}
	if err := store.AdvanceCursor(ctx, sym, 4, "owner-b", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceCursor(ctx, sym, 5, "owner-a", time.Minute); err == nil {
		t.Fatal("previous owner advanced a lease it no longer holds")
	}
	got, err := store.GetCursor(ctx, sym)
	if err != nil || got != 4 {
		t.Fatalf("cursor=%d err=%v", got, err)
	}
}

func TestCursorLeaseSeconds(t *testing.T) {
	if leaseSeconds(0) != 1 {
		t.Fatal("zero ttl")
	}
	if leaseSeconds(1500*time.Millisecond) != 1 {
		t.Fatal("truncated second")
	}
	if leaseSeconds(15*time.Second) != 15 {
		t.Fatal("15s")
	}
}
