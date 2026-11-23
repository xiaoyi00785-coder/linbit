package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

type fakeMover struct {
	snap     ColdSnap
	snapErr  error
	sendHash string
	sendErr  error
	destSeen string
	amount   decimal.Decimal
	sends    int
	confs    int64
	failed   bool
	confErr  error
}

func (f *fakeMover) Snapshot(context.Context, string, string) (ColdSnap, error) {
	return f.snap, f.snapErr
}
func (f *fakeMover) SendToConfiguredCold(_ context.Context, _, _, dest string, amount decimal.Decimal) (string, error) {
	f.sends++
	f.destSeen = dest
	f.amount = amount
	return f.sendHash, f.sendErr
}
func (f *fakeMover) ConfirmTx(context.Context, string, string) (int64, bool, error) {
	return f.confs, f.failed, f.confErr
}

func TestComputeColdSend(t *testing.T) {
	bal := d("100")
	got, err := computeColdSend(ctModeAmount, d("40"), d("0"), bal)
	if err != nil || got.String() != "40" {
		t.Fatalf("amount: got %s err=%v", got, err)
	}
	if _, err := computeColdSend(ctModeAmount, d("100.1"), d("0"), bal); err == nil {
		t.Fatal("want exceed error")
	}
	if _, err := computeColdSend(ctModeAmount, d("0"), d("0"), bal); err == nil {
		t.Fatal("want positive amount")
	}
	got, err = computeColdSend(ctModeReserve, d("0"), d("15"), bal)
	if err != nil || got.String() != "85" {
		t.Fatalf("reserve: got %s err=%v", got, err)
	}
	if _, err := computeColdSend(ctModeReserve, d("0"), d("100"), bal); err == nil {
		t.Fatal("want reserve-too-high")
	}
	if _, err := computeColdSend("other", d("1"), d("0"), bal); err == nil {
		t.Fatal("want bad mode")
	}
}

func TestSameChainAddr(t *testing.T) {
	if !sameChainAddr("0xABC", "0xabc") {
		t.Fatal("EVM should be case-insensitive")
	}
	if sameChainAddr("TBKPp4yKdw2g1VQWLWbH19z1h7PYPGhz7C", "Tother") {
		t.Fatal("tron mismatch")
	}
	if sameChainAddr("", "x") || sameChainAddr("x", "") {
		t.Fatal("empty")
	}
}

func TestValidateGas(t *testing.T) {
	native := ColdSnap{Balance: d("10"), NativeBalance: d("10"), NativeAsset: "ETH", GasEstimate: d("0.01"), NativeIsAsset: true}
	if err := validateGas(native, d("9.99")); err != nil {
		t.Fatalf("native just enough: %v", err)
	}
	if err := validateGas(native, d("10")); err == nil {
		t.Fatal("native amount=balance should fail (no gas left)")
	}
	tok := ColdSnap{Balance: d("50"), NativeBalance: d("0.001"), NativeAsset: "ETH", GasEstimate: d("0.002"), NativeIsAsset: false}
	if err := validateGas(tok, d("10")); err == nil {
		t.Fatal("token should fail on gas")
	}
	tok.NativeBalance = d("0.01")
	if err := validateGas(tok, d("10")); err != nil {
		t.Fatalf("token with gas: %v", err)
	}
}

func TestCheckColdMaxUSDT(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "1000")
	s := &Server{cfg: Config{}}
	if err := s.checkColdMax(context.Background(), "USDT", "TRC20", d("1000")); err != nil {
		t.Fatalf("at cap: %v", err)
	}
	if err := s.checkColdMax(context.Background(), "USDT", "TRC20", d("1000.01")); err == nil || err.code != "OVER_MAX" {
		t.Fatalf("over cap: %+v", err)
	}
}

func TestCheckColdMaxAssetEnv(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "1000000")
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT_TRC20", "50")
	s := &Server{cfg: Config{}}
	if err := s.checkColdMax(context.Background(), "USDT", "TRC20", d("51")); err == nil || err.code != "OVER_MAX" {
		t.Fatalf("pair cap: %+v", err)
	}
}

func testColdServer(t *testing.T, mover *fakeMover) *Server {
	t.Helper()
	store, _ := newTestStore(t)
	s := NewServer(testServerConfig(t, "", ""), store)
	s.coldMove = mover
	return s
}

func defaultSnap() ColdSnap {
	return ColdSnap{
		Asset: "USDT", Network: "TRC20", Chain: "tron",
		Hot:     "THotxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		Cold:    "TColdxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		Balance: d("500"), NativeBalance: d("100"), NativeAsset: "TRX",
		GasEstimate: d("1"), NativeIsAsset: false,
	}
}

func TestCreateIgnoresCustomDestinationAndIdempotency(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "100000")
	mv := &fakeMover{snap: defaultSnap()}
	s := testColdServer(t, mv)
	req := coldTransferCreateReq{
		Asset: "USDT", Network: "TRC20", Mode: ctModeAmount, Amount: "20",
		Reason: "quarterly cold storage", InitiatedBy: 1, InitiatedByName: "alice",
		IdempotencyKey: "idem-1", Destination: "TAttackerShouldBeIgnoredxxxxxxxxxxxx",
	}
	row, err := s.createColdTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("create: %+v", err)
	}
	if row.Destination != mv.snap.Cold {
		t.Fatalf("dest = %q, want configured cold %q (custom address must be ignored)", row.Destination, mv.snap.Cold)
	}
	if row.Status != ctPending || row.Amount.String() != "20" {
		t.Fatalf("row = %+v", row)
	}
	again, err := s.createColdTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %+v", err)
	}
	if again.ID != row.ID {
		t.Fatalf("idempotency: got id %d want %d", again.ID, row.ID)
	}
	req.IdempotencyKey = "idem-2"
	if _, err := s.createColdTransfer(context.Background(), req); err == nil || err.code != "IN_FLIGHT" {
		t.Fatalf("second in-flight: %+v", err)
	}
}

func TestReserveModeAndSelfReviewAndApproveBroadcast(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "100000")
	mv := &fakeMover{snap: defaultSnap(), sendHash: "txid-cold-1", confs: 0}
	s := testColdServer(t, mv)
	row, err := s.createColdTransfer(context.Background(), coldTransferCreateReq{
		Asset: "usdt", Network: "trc20", Mode: ctModeReserve, ReserveAmount: "100",
		Reason: "leave some gas-adjacent usdt", InitiatedBy: 7, InitiatedByName: "alice",
		IdempotencyKey: "idem-reserve",
	})
	if err != nil {
		t.Fatalf("create: %+v", err)
	}
	if row.Amount.String() != "400" { // 500-100
		t.Fatalf("send amount = %s, want 400", row.Amount)
	}

	// Initiator cannot approve.
	ok, e := s.store.ApproveHotColdTransfer(context.Background(), row.ID, 7, "alice")
	if e != nil || ok {
		t.Fatalf("self approve applied: ok=%v err=%v", ok, e)
	}

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(coldTransferDecisionReq{ActorID: 7, ActorName: "alice"})
	req := httptest.NewRequest(http.MethodPost, "/internal/admin/wallet/cold-transfers/"+itoa64(row.ID)+"/approve", bytes.NewReader(body))
	req.SetPathValue("id", itoa64(row.ID))
	s.handleColdTransferApprove(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("self review HTTP %d body=%s", rec.Code, rec.Body.Bytes())
	}

	rec = httptest.NewRecorder()
	body, _ = json.Marshal(coldTransferDecisionReq{ActorID: 8, ActorName: "bob"})
	req = httptest.NewRequest(http.MethodPost, "/internal/admin/wallet/cold-transfers/"+itoa64(row.ID)+"/approve", bytes.NewReader(body))
	req.SetPathValue("id", itoa64(row.ID))
	s.handleColdTransferApprove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve HTTP %d body=%s", rec.Code, rec.Body.Bytes())
	}

	b, c, err2 := s.ProcessColdTransfers(context.Background())
	if err2 != nil || b != 1 {
		t.Fatalf("broadcast: n=%d complete=%d err=%v sends=%d", b, c, err2, mv.sends)
	}
	if mv.sends != 1 || mv.destSeen != mv.snap.Cold || mv.amount.String() != "400" {
		t.Fatalf("send dest=%s amt=%s sends=%d", mv.destSeen, mv.amount, mv.sends)
	}
	got, _ := s.store.GetHotColdTransfer(context.Background(), row.ID)
	if got.Status != ctProcessing || got.TxHash != "txid-cold-1" {
		t.Fatalf("after broadcast: %+v", got)
	}

	// Not enough confs yet.
	mv.confs = 2
	if _, c, _ = s.ProcessColdTransfers(context.Background()); c != 0 {
		t.Fatalf("early complete %d", c)
	}
	// Registry default in testServerConfig is 3 confirmations.
	mv.confs = 3
	if _, c, _ = s.ProcessColdTransfers(context.Background()); c != 1 {
		t.Fatalf("complete n=%d", c)
	}
	got, _ = s.store.GetHotColdTransfer(context.Background(), row.ID)
	if got.Status != ctCompleted || got.Confirmations != 3 {
		t.Fatalf("completed: %+v", got)
	}
}

func TestBroadcastRefusesChangedColdAddress(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "100000")
	mv := &fakeMover{snap: defaultSnap(), sendHash: "should-not-send"}
	s := testColdServer(t, mv)
	row, err := s.createColdTransfer(context.Background(), coldTransferCreateReq{
		Asset: "USDT", Network: "TRC20", Mode: ctModeAmount, Amount: "10",
		Reason: "move", InitiatedBy: 1, InitiatedByName: "alice", IdempotencyKey: "idem-change",
	})
	if err != nil {
		t.Fatalf("create: %+v", err)
	}
	if ok, e := s.store.ApproveHotColdTransfer(context.Background(), row.ID, 2, "bob"); !ok || e != nil {
		t.Fatalf("approve: %v %v", ok, e)
	}
	mv.snap.Cold = "TNewColdxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	if b, _, _ := s.ProcessColdTransfers(context.Background()); b != 0 {
		t.Fatalf("broadcast despite cold change: %d", b)
	}
	if mv.sends != 0 {
		t.Fatalf("must not sign/broadcast when cold address changed, sends=%d", mv.sends)
	}
	got, _ := s.store.GetHotColdTransfer(context.Background(), row.ID)
	if got.Status != ctFailed {
		t.Fatalf("status=%s want failed", got.Status)
	}
}

func TestCreateRejectsMissingColdAndInsufficientGas(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "100000")
	mv := &fakeMover{snap: defaultSnap()}
	mv.snap.Cold = ""
	s := testColdServer(t, mv)
	if _, err := s.createColdTransfer(context.Background(), coldTransferCreateReq{
		Asset: "USDT", Network: "TRC20", Mode: ctModeAmount, Amount: "10",
		Reason: "move", InitiatedBy: 1, InitiatedByName: "a", IdempotencyKey: "k1",
	}); err == nil || err.code != "NO_COLD_ADDRESS" {
		t.Fatalf("missing cold: %+v", err)
	}
	mv.snap.Cold = defaultSnap().Cold
	mv.snap.NativeBalance = d("0")
	if _, err := s.createColdTransfer(context.Background(), coldTransferCreateReq{
		Asset: "USDT", Network: "TRC20", Mode: ctModeAmount, Amount: "10",
		Reason: "move", InitiatedBy: 1, InitiatedByName: "a", IdempotencyKey: "k2",
	}); err == nil || err.code != "INSUFFICIENT_GAS" {
		t.Fatalf("gas: %+v", err)
	}
}

func TestColdTransferHTTPQuoteAndRoutes(t *testing.T) {
	t.Setenv("WALLET_COLD_TRANSFER_MAX_USDT", "100000")
	mv := &fakeMover{snap: defaultSnap()}
	s := testColdServer(t, mv)
	h := s.Routes()
	req := httptest.NewRequest(http.MethodGet, "/internal/admin/wallet/cold-transfers/quote?asset=USDT&network=TRC20", nil)
	req.Header.Set("X-Internal-Token", "tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote %d %s", rec.Code, rec.Body.Bytes())
	}
	if !strings.Contains(rec.Body.String(), mv.snap.Cold) || strings.Contains(rec.Body.String(), `"destination":"TAttacker`) {
		t.Fatalf("quote body: %s", rec.Body.Bytes())
	}
	var q struct {
		Snapshot ColdSnap `json:"snapshot"`
		InFlight bool     `json:"inFlight"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	if q.Snapshot.Hot != mv.snap.Hot || q.InFlight {
		t.Fatalf("quote parse: %+v", q)
	}
}

func TestRealSendToConfiguredColdRejectsWrongDest(t *testing.T) {
	// A zero RealChain (no nets) should error; the dest check is also unit-tested
	// via sameChainAddr + broadcast refusal above. This guards the method exists
	// and never sends when lookup fails (no signer called).
	rc := &RealChain{nets: map[string]*chainNet{}}
	if _, err := rc.SendToConfiguredCold(context.Background(), "USDT", "TRC20", "Twhatever", d("1")); err == nil {
		t.Fatal("want lookup error, not a send")
	}
}
