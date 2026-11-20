package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestExportTradesCSVEndToEnd enqueues a trades CSV export over HTTP, runs one
// worker lease, and downloads the resulting file via the signed URL.
func TestExportTradesCSVEndToEnd(t *testing.T) {
	s := newTestServer(t)
	uid := testUser(t)
	cleanupUser(t, s, uid)
	ctx := context.Background()

	// Seed two trades for this user.
	for i, side := range []string{"buy", "sell"} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO trades
			(trade_id, order_id, user_id, symbol, side, price, quantity, fee, fee_asset, role)
			VALUES ($1,$2,$3,'BTCUSDT',$4,'10000','0.1','0.5','USDT','taker')`,
			"tid-"+itoa(i), "oid-"+itoa(i), uid, side); err != nil {
			t.Fatalf("seed trade: %v", err)
		}
	}

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// Create the export job via the HTTP API.
	body := strings.NewReader(`{"type":"trades","format":"csv"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/reports/exports", body)
	req.Header.Set("X-User-Id", uid)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST exports: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status: %d", resp.StatusCode)
	}
	var job exportDTO
	_ = json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	if job.JobID == "" || job.Status != "queued" {
		t.Fatalf("unexpected job: %+v", job)
	}

	// Lease and run the worker synchronously.
	j, err := s.store.LeaseNextExport(ctx)
	if err != nil || j == nil {
		t.Fatalf("lease: %v / %v", err, j)
	}
	s.runExport(ctx, j)

	// GET the job to confirm it ready, then download.
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/reports/exports/"+job.JobID, nil)
	req.Header.Set("X-User-Id", uid)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET export: %v", err)
	}
	var ready exportDTO
	_ = json.NewDecoder(resp.Body).Decode(&ready)
	resp.Body.Close()
	if ready.Status != "ready" || ready.DownloadURL == "" || ready.RowCount == nil || *ready.RowCount != 2 {
		t.Fatalf("not ready: %+v", ready)
	}
	// DownloadURL is relative; prepend the test server host so http.Get works.
	downURL := ts.URL + ready.DownloadURL
	resp, err = http.Get(downURL)
	if err != nil {
		t.Fatalf("GET download: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status %d: %s", resp.StatusCode, data)
	}
	// Strip BOM, parse.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("csv rows: want 3 got %d", len(rows))
	}
	if rows[0][1] != "symbol" {
		t.Fatalf("header mismatch: %v", rows[0])
	}
	if rows[1][1] != "BTCUSDT" {
		t.Fatalf("row[1] symbol: %v", rows[1])
	}

	// Bad token must 403.
	resp, _ = http.Get(ts.URL + "/reports/exports/" + ready.JobID + "/download?token=wrong")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad token status: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestExportXLSXAndPDFFormats(t *testing.T) {
	s := newTestServer(t)
	uid := testUser(t)
	cleanupUser(t, s, uid)
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx, `INSERT INTO deposits
		(user_id, asset, network, address, amount, tx_hash, log_index, confirmations, status, credited_at)
		VALUES ($1,'USDT','TRC20','TXaddr','123.4','0xhash',0,12,'completed',now())`, uid); err != nil {
		t.Fatalf("seed deposit: %v", err)
	}

	runJob := func(kind, format string) *ExportJob {
		j := &ExportJob{JobID: randomID(), UserID: uid, Kind: kind, Format: format}
		if err := s.store.InsertExport(ctx, j); err != nil {
			t.Fatalf("insert %s/%s: %v", kind, format, err)
		}
		// Lease by jobId directly rather than LeaseNextExport, which is
		// unordered across users and could pick up a leftover from another test.
		if _, err := s.db.ExecContext(ctx,
			`UPDATE report_exports SET status='running', started_at=now() WHERE job_id=$1`, j.JobID); err != nil {
			t.Fatalf("lease %s: %v", j.JobID, err)
		}
		got, err := s.store.GetExport(ctx, uid, j.JobID)
		if err != nil || got == nil {
			t.Fatalf("get after lease %s: %v / %v", j.JobID, err, got)
		}
		s.runExport(ctx, got)
		ready, err := s.store.GetExport(ctx, uid, j.JobID)
		if err != nil || ready == nil {
			t.Fatalf("get after run %s: %v / %v", j.JobID, err, ready)
		}
		return ready
	}

	for _, format := range []string{"csv", "xlsx"} {
		got := runJob("deposits", format)
		if got.Status != "ready" {
			t.Fatalf("%s: status %s, err=%s", format, got.Status, got.Error.String)
		}
		if _, err := os.Stat(got.FilePath.String); err != nil {
			t.Fatalf("%s file missing: %v", format, err)
		}
	}
	got := runJob("ledger", "pdf")
	if got.Status != "ready" {
		t.Fatalf("pdf not ready: %s, err=%s", got.Status, got.Error.String)
	}
	data, _ := os.ReadFile(got.FilePath.String)
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("pdf not a PDF")
	}
}

func TestTaxReportPDF(t *testing.T) {
	s := newTestServer(t)
	uid := testUser(t)
	cleanupUser(t, s, uid)
	ctx := context.Background()

	// Seed one buy and one sell at different prices in 2025 so realized
	// capital gains come out non-zero.
	ins := func(side, price string) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO trades
			(trade_id, order_id, user_id, symbol, side, price, quantity, fee, fee_asset, role, created_at)
			VALUES ($1,$2,$3,'BTCUSDT',$4,$5,'0.1','0.1','USDT','taker','2025-05-01')`,
			"t-"+side, "o-"+side, uid, side, price); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ins("buy", "10000")
	ins("sell", "12000")

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	body := strings.NewReader(`{"year":2025,"country":"US"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/reports/tax", body)
	req.Header.Set("X-User-Id", uid)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST tax: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status: %d", resp.StatusCode)
	}
	var job exportDTO
	_ = json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	if job.Format != "pdf" {
		t.Fatalf("format: %s", job.Format)
	}

	leased, _ := s.store.LeaseNextExport(ctx)
	s.runExport(ctx, leased)
	got, _ := s.store.GetExport(ctx, uid, leased.JobID)
	if got.Status != "ready" {
		t.Fatalf("tax not ready: %s / %s", got.Status, got.Error.String)
	}
	data, _ := os.ReadFile(got.FilePath.String)
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("tax not a PDF")
	}
	if len(data) < 2000 {
		t.Fatalf("tax PDF too small: %d", len(data))
	}
}

// TestSubscriptionDepositEndToEnd seeds a subscription and a matching deposit,
// runs the watcher, and asserts that the account service received a Notice
// for the right user.
func TestSubscriptionDepositEndToEnd(t *testing.T) {
	s := newTestServer(t)
	uid := testUser(t)
	cleanupUser(t, s, uid)
	ctx := context.Background()

	acct := newFakeAccount(t)
	os.Setenv("ACCOUNT_INTERNAL_URL", acct.srv.URL)
	os.Setenv("INTERNAL_TOKEN", "test-token")
	t.Cleanup(func() {
		os.Unsetenv("ACCOUNT_INTERNAL_URL")
		os.Unsetenv("INTERNAL_TOKEN")
	})

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	// Create a subscription over HTTP: asset=USDT, chain=TRC20, min 100.
	body := strings.NewReader(`{"asset":"USDT","chain":"TRC20","minAmount":"100","channel":"push"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/notifications/subscriptions", body)
	req.Header.Set("X-User-Id", uid)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST sub: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Listing must include the subscription.
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/notifications/subscriptions", nil)
	req.Header.Set("X-User-Id", uid)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET subs: %v", err)
	}
	var list struct{ Items []subDTO }
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Items) != 1 || list.Items[0].Asset != "USDT" {
		t.Fatalf("subs list: %+v", list)
	}

	// A below-threshold deposit must NOT trigger a notification.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO deposits
		(user_id, asset, network, address, amount, tx_hash, status, credited_at)
		VALUES ($1,'USDT','TRC20','TXsub1','5','0xtx1','completed',now())`, uid); err != nil {
		t.Fatalf("seed small: %v", err)
	}
	s.depositTick(ctx)
	if n := acct.count(); n != 0 {
		t.Fatalf("expected 0 notices for below-threshold deposit, got %d", n)
	}

	// An above-threshold deposit must trigger exactly one notification. Notify
	// is fire-and-forget (background goroutine) so give it a short beat.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO deposits
		(user_id, asset, network, address, amount, tx_hash, status, credited_at)
		VALUES ($1,'USDT','TRC20','TXsub2','250','0xtx2','completed',now())`, uid); err != nil {
		t.Fatalf("seed big: %v", err)
	}
	s.depositTick(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if acct.count() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := acct.count(); n != 1 {
		t.Fatalf("expected 1 notice, got %d", n)
	}

	// DELETE the subscription.
	subID := list.Items[0].ID
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/notifications/subscriptions/"+subID, nil)
	req.Header.Set("X-User-Id", uid)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestEventsWebSocket connects to /account/events and confirms the deposit
// watcher pushes a frame on the stream.
func TestEventsWebSocket(t *testing.T) {
	s := newTestServer(t)
	uid := testUser(t)
	cleanupUser(t, s, uid)
	ctx := context.Background()

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/account/events"

	// Connect with X-User-Id header (the gateway-like path).
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-User-Id": []string{uid}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	rctx, rcancel := context.WithTimeout(ctx, 3*time.Second)
	defer rcancel()
	_, hello, err := c.Read(rctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if !strings.Contains(string(hello), `"type":"hello"`) {
		t.Fatalf("hello frame: %s", hello)
	}

	// Seed a deposit, tick, and expect one frame.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO deposits
		(user_id, asset, network, address, amount, tx_hash, status, credited_at)
		VALUES ($1,'BTC','BTC','addr','0.25','0xws1','completed',now())`, uid); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s.depositTick(ctx)
	rctx2, rcancel2 := context.WithTimeout(ctx, 3*time.Second)
	defer rcancel2()
	_, frame, err := c.Read(rctx2)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if !strings.Contains(string(frame), `"type":"deposit"`) {
		t.Fatalf("unexpected frame: %s", frame)
	}
}

func itoa(i int) string {
	return "x" + string(rune('A'+i))
}
