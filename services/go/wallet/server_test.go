package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeAsset records asset-internal calls and answers 200.
type fakeAssetHits struct {
	freeze, unfreeze, deposit, complete int32
}

func newFakeAssetFull(t *testing.T, hits *fakeAssetHits) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/freeze":
			atomic.AddInt32(&hits.freeze, 1)
		case "/internal/unfreeze":
			atomic.AddInt32(&hits.unfreeze, 1)
		case "/internal/deposit":
			atomic.AddInt32(&hits.deposit, 1)
		case "/internal/withdraw-complete":
			atomic.AddInt32(&hits.complete, 1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeAccount answers the internal 2FA + whitelist endpoints.
func newFakeAccount(t *testing.T, ok2fa bool, wlEnabled, wlAllowed bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/withdraw/verify":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok2fa})
		case "/internal/withdraw/whitelist":
			_ = json.NewEncoder(w).Encode(map[string]any{"enabled": wlEnabled, "allowed": wlAllowed})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testServerConfig(t *testing.T, assetURL, accountURL string) Config {
	cfg := Config{
		InternalToken:      "tok",
		AssetInternalURL:   assetURL,
		AccountInternalURL: accountURL,
		FeeAccount:         "platform:gas",
		GasDeductMode:      "extra",
		CreditBelowMinDeposit: true,
		Risk: RiskThresholds{
			AutoMaxAmount: d("1000"), AutoMaxScore: 20,
			ReviewMinAmount: d("5000"), ReviewMaxScore: 40, MiddleAction: "review",
		},
		Networks: newRegistry([]Network{{
			Asset: "USDT", Network: "TRC20", Confirmations: 3,
			MinDeposit: d("1"), MinWithdraw: d("12"), MaxWithdraw: d("20000"),
			GasFeeEstimate: d("2"), GasFeeAsset: "USDT", EstimatedArrivalMinutes: 4,
		}}),
	}
	return cfg
}

// TestSubmitWhitelistBlocked: with the whitelist enabled and the destination not
// whitelisted, submit is rejected with 403 and passes no freeze.
func TestSubmitWhitelistBlocked(t *testing.T) {
	store, _ := newTestStore(t)
	hits := &fakeAssetHits{}
	assetSrv := newFakeAssetFull(t, hits)
	acctSrv := newFakeAccount(t, true, true /*enabled*/, false /*allowed*/)
	s := NewServer(testServerConfig(t, assetSrv.URL, acctSrv.URL), store)

	_, apiErr := s.submitWithdrawal(context.Background(), testUser(), withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Tblocked", Amount: d("100"),
	})
	if apiErr == nil || apiErr.code != "WHITELIST_BLOCKED" || apiErr.status != http.StatusForbidden {
		t.Fatalf("apiErr = %+v, want WHITELIST_BLOCKED/403", apiErr)
	}
	if hits.freeze != 0 {
		t.Fatalf("freeze called %d times, want 0", hits.freeze)
	}
}

// TestSubmitAutoVsReview: a small low-risk withdrawal auto-releases (approved,
// funds frozen); a large one is held for review. A withdrawal below the minimum
// is rejected before any side effects.
func TestSubmitAutoVsReview(t *testing.T) {
	store, _ := newTestStore(t)
	hits := &fakeAssetHits{}
	assetSrv := newFakeAssetFull(t, hits)
	acctSrv := newFakeAccount(t, true, false, true)
	s := NewServer(testServerConfig(t, assetSrv.URL, acctSrv.URL), store)
	user := testUser()

	// Small amount: risk score 0+20 (new address+new device) = 20 ≤ 20, amount ≤1000 → auto.
	wd, apiErr := s.submitWithdrawal(context.Background(), user, withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Tsmall", Amount: d("100"),
	})
	if apiErr != nil {
		t.Fatalf("small submit: %+v", apiErr)
	}
	if wd.Status != wStatusApproved {
		t.Fatalf("small status = %q, want approved (auto)", wd.Status)
	}
	// Frozen = amount + gas (extra mode).
	if wd.FrozenAmount.String() != "102" {
		t.Fatalf("frozen = %s, want 102", wd.FrozenAmount)
	}

	// Large amount (>5000) forces review even at the same risk score.
	wd, apiErr = s.submitWithdrawal(context.Background(), user, withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Tlarge", Amount: d("6000"),
	})
	if apiErr != nil {
		t.Fatalf("large submit: %+v", apiErr)
	}
	if wd.Status != wStatusReview {
		t.Fatalf("large status = %q, want pending_review", wd.Status)
	}
	if hits.freeze != 2 {
		t.Fatalf("freeze called %d times, want 2", hits.freeze)
	}

	// Below the 12 USDT minimum is rejected with no freeze.
	if _, apiErr := s.submitWithdrawal(context.Background(), user, withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Ttiny", Amount: d("5"),
	}); apiErr == nil || apiErr.code != "MIN_WITHDRAW" {
		t.Fatalf("tiny submit = %+v, want MIN_WITHDRAW", apiErr)
	}
	if hits.freeze != 2 {
		t.Fatalf("freeze called %d times after tiny, want 2", hits.freeze)
	}
}

// TestSubmitRequires2FA: a failed second factor blocks submission.
func TestSubmitRequires2FA(t *testing.T) {
	store, _ := newTestStore(t)
	hits := &fakeAssetHits{}
	assetSrv := newFakeAssetFull(t, hits)
	acctSrv := newFakeAccount(t, false, false, true) // 2FA fails
	s := NewServer(testServerConfig(t, assetSrv.URL, acctSrv.URL), store)

	_, apiErr := s.submitWithdrawal(context.Background(), testUser(), withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Tx", Amount: d("100"),
	})
	if apiErr == nil || apiErr.code != "TWOFA_REQUIRED" {
		t.Fatalf("apiErr = %+v, want TWOFA_REQUIRED", apiErr)
	}
	if hits.freeze != 0 {
		t.Fatalf("freeze called %d times, want 0", hits.freeze)
	}
}

// TestRejectUnfreezes: a manual review rejected by an admin releases the frozen
// funds exactly once.
func TestRejectUnfreezes(t *testing.T) {
	store, _ := newTestStore(t)
	hits := &fakeAssetHits{}
	assetSrv := newFakeAssetFull(t, hits)
	acctSrv := newFakeAccount(t, true, false, true)
	s := NewServer(testServerConfig(t, assetSrv.URL, acctSrv.URL), store)

	wd, apiErr := s.submitWithdrawal(context.Background(), testUser(), withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Tbig", Amount: d("6000"),
	})
	if apiErr != nil || wd.Status != wStatusReview {
		t.Fatalf("submit: %+v status=%v", apiErr, wd)
	}
	if ok, err := s.adminReject(context.Background(), wd.ID, "kyc"); !ok || err != nil {
		t.Fatalf("reject: ok=%v err=%v", ok, err)
	}
	if hits.unfreeze != 1 {
		t.Fatalf("unfreeze = %d, want 1", hits.unfreeze)
	}
}

// TestBroadcastAndComplete drives an approved withdrawal through broadcast and
// confirmation, asserting the frozen funds are settled (completed) once.
func TestBroadcastAndComplete(t *testing.T) {
	store, _ := newTestStore(t)
	hits := &fakeAssetHits{}
	assetSrv := newFakeAssetFull(t, hits)
	acctSrv := newFakeAccount(t, true, false, true)
	s := NewServer(testServerConfig(t, assetSrv.URL, acctSrv.URL), store)

	wd, apiErr := s.submitWithdrawal(context.Background(), testUser(), withdrawInput{
		Asset: "USDT", Network: "TRC20", Address: "Tok", Amount: d("100"),
	})
	if apiErr != nil || wd.Status != wStatusApproved {
		t.Fatalf("submit: %+v status=%v", apiErr, wd)
	}
	// Broadcast moves approved -> processing.
	if _, _, err := s.ProcessWithdrawals(context.Background()); err != nil {
		t.Fatalf("process: %v", err)
	}
	got, _ := store.GetWithdrawal(context.Background(), wd.ID)
	if got.Status != wStatusProcessing {
		t.Fatalf("after broadcast status = %q, want processing", got.Status)
	}
	// Advance to 3 confirmations, then process again to complete.
	if _, err := store.AdvanceChain(context.Background(), "TRC20", 2); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, _, err := s.ProcessWithdrawals(context.Background()); err != nil {
		t.Fatalf("process2: %v", err)
	}
	got, _ = store.GetWithdrawal(context.Background(), wd.ID)
	if got.Status != wStatusCompleted {
		t.Fatalf("after confirm status = %q, want completed", got.Status)
	}
	if hits.complete != 1 {
		t.Fatalf("withdraw-complete calls = %d, want 1", hits.complete)
	}
	if apiStatus(got.Status) != "completed" {
		t.Fatalf("api status = %q", apiStatus(got.Status))
	}
}
