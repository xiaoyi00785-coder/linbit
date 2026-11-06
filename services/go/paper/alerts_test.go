package main

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"context"
	"errors"
	"fmt"
	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
	"sync"
	"testing"
	"time"
)

type fakeNotifier struct {
	mu    sync.Mutex
	ids   map[string]string
	calls int
}

func (n *fakeNotifier) InApp(_ context.Context, uid, title, body, key string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if n.ids == nil {
		n.ids = map[string]string{}
	}
	if id, ok := n.ids[key]; ok {
		return id, nil
	}
	id := fmt.Sprintf("notice-%d", n.calls)
	n.ids[key] = id
	return id, nil
}

func newAlert(t *testing.T, store *Store, m *fakeMarket, user, id string, condition, threshold string) PriceAlert {
	t.Helper()
	a, err := store.CreateAlert(context.Background(), user, CreatePriceAlert{ClientAlertID: id, Market: "spot", Symbol: "BTCUSDT", Condition: condition, Threshold: threshold, PriceSource: "INDEX", TriggerMode: "once", Channels: []string{"in_app"}, Note: "test"}, m, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func newTestServer(store *Store, m Market, n NotificationSender) *Server {
	cfg := Config{JWTSecret: "tests", AlertCooldown: 300 * time.Second, AlertWriteLimit: 20, AlertWriteWindow: time.Minute}
	return NewServer(cfg, store, NewFeatureGate(cfg, store), m, n)
}

func TestAlertOnceCrossRevisionAndDeliveryRecovery(t *testing.T) {
	store := migratedStore(t)
	user := "80000001"
	m := testMarket("50000.00")
	notifier := &fakeNotifier{}
	srv := newTestServer(store, m, notifier)
	ctx := context.Background()
	a := newAlert(t, store, m, user, "alert-replay", "gte", "80000.00")
	same := CreatePriceAlert{ClientAlertID: "alert-replay", Market: "spot", Symbol: "BTCUSDT", Condition: "gte", Threshold: "80000.00", PriceSource: "INDEX", TriggerMode: "once", Channels: []string{"in_app"}, Note: "test"}
	got, err := store.CreateAlert(ctx, user, same, m, time.Now())
	if err != nil || got.AlertID != a.AlertID {
		t.Fatalf("create replay: %#v %v", got, err)
	}
	m.quote = Quote{Symbol: "BTCUSDT", Price: decimal.RequireFromString("81000.00"), ObservedAt: time.Now().UTC()}
	if err := srv.evaluateSymbol(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if err := srv.evaluateSymbol(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM price_alert_trigger_history WHERE alert_id=$1`, a.AlertID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("triggers=%d err=%v", n, err)
	}
	if err := srv.deliverAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	if err := srv.deliverAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("duplicate notices=%d", notifier.calls)
	}
	got, err = alertByIDTx(ctx, store.db, user, a.AlertID, false)
	if err != nil || got.Status != "triggered" || got.DeliveryStatus != "delivered_in_app" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	stale := a.Revision
	if _, err := store.UpdateAlert(ctx, user, a.AlertID, UpdatePriceAlert{Revision: &stale, Threshold: strptr("82000.00")}, m, time.Now()); apiCode(err) != "REVISION_CONFLICT" {
		t.Fatalf("stale patch err=%v", err)
	}
	revision := got.Revision
	active, err := store.UpdateAlert(ctx, user, a.AlertID, UpdatePriceAlert{Revision: &revision, Threshold: strptr("82000.00"), Enabled: boolptr(true)}, m, time.Now())
	if err != nil || active.Status != "active" || active.LastObservedPrice != nil || active.Revision <= got.Revision {
		t.Fatalf("new epoch=%#v err=%v", active, err)
	}
	if err := srv.evaluateSymbol(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("new epoch baseline triggered incorrectly: %d", notifier.calls)
	}
	if err := store.DeleteAlert(ctx, user, a.AlertID, &active.Revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAlert(ctx, user, a.AlertID, &active.Revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := alertByIDTx(ctx, store.db, user, a.AlertID, false); apiCode(err) != "ALERT_NOT_FOUND" {
		t.Fatalf("deleted visible: %v", err)
	}
}
func strptr(v string) *string { return &v }
func boolptr(v bool) *bool    { return &v }

func TestAlertNotificationWriteRepairAndFailClosedHTTP(t *testing.T) {
	store := migratedStore(t)
	m := testMarket("50000.00")
	a := newAlert(t, store, m, "80000001", "outbox-repair", "lte", "50000.00")
	now := time.Now().UTC()
	if _, err := store.db.Exec(`UPDATE price_alerts SET status='triggered',enabled=false,delivery_status='pending',notification_id='notice-existing' WHERE alert_id=$1`, a.AlertID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO price_alert_outbox (alert_id,evaluation_epoch,user_id,symbol,condition,threshold,observed_price,observed_at,state,next_attempt_at,created_at,updated_at) VALUES ($1,$2,$3,$4,'lte',50000,50000,$5,'delivered',$5,$5,$5)`, a.AlertID, a.Revision, "80000001", "BTCUSDT", now); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(store, m, &fakeNotifier{})
	if err := srv.deliverAlerts(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := alertByIDTx(context.Background(), store.db, "80000001", a.AlertID, false)
	if err != nil || got.DeliveryStatus != "delivered_in_app" {
		t.Fatalf("repair: %#v %v", got, err)
	}
	if _, err := store.db.Exec(`DELETE FROM app_features WHERE key='priceAlerts'`); err != nil {
		t.Fatal(err)
	}
	token, _ := common.SignAccessToken("tests", "80000001", "a@example.test", time.Hour, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/price-alerts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "FEATURE_DISABLED") {
		t.Fatalf("flag off code=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := store.db.Exec(`INSERT INTO app_features VALUES ('priceAlerts',TRUE)`); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/price-alerts", nil)
	req.Header.Set("X-User-Id", "80000001")
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth code=%d", rr.Code)
	}
}

func TestConcurrentAlertAndResetIdempotency(t *testing.T) {
	store := migratedStore(t)
	m := testMarket("50000.00")
	const workers = 4
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := store.Reset(context.Background(), "80000001", "concurrent-reset", "RESET_PAPER_ACCOUNT")
			if err != nil {
				errs <- err
				return
			}
			if response.Generation != 2 {
				errs <- fmt.Errorf("unexpected generation %d", response.Generation)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var resets int
	if err := store.db.QueryRow(`SELECT generation,resets_used_today FROM paper_accounts WHERE user_id='80000001'`).Scan(&resets, &resets); err != nil || resets != 1 {
		t.Fatalf("reset replay did not preserve single reset: %v", err)
	}
	// Independent alert create replay uses the same serialization boundary.
	errs = make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.CreateAlert(context.Background(), "80000002", CreatePriceAlert{ClientAlertID: "same-alert", Symbol: "BTCUSDT", Condition: "gte", Threshold: "60000.00"}, m, time.Now())
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM price_alerts WHERE user_id='80000002' AND client_alert_id='same-alert'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent alerts=%d, err=%v", count, err)
	}
}

func TestAlertWriteRateLimitAcrossUsersAndServerRestart(t *testing.T) {
	store := migratedStore(t)
	m := testMarket("50000.00")
	srv := newTestServer(store, m, &fakeNotifier{})
	srv.cfg.AlertWriteLimit = 3
	token := func(uid string) string {
		value, err := common.SignAccessToken("tests", uid, uid+"@example.test", time.Hour, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	do := func(method, path, body, uid string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token(uid))
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		return rr
	}
	create := func(id, uid string) string {
		return `{"clientAlertId":"` + id + `","symbol":"BTCUSDT","condition":"gte","threshold":"60000.00"}`
	}
	if rr := do(http.MethodPost, "/price-alerts", create("rate-a", "80000001"), "80000001"); rr.Code != http.StatusCreated {
		t.Fatalf("first create code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodPost, "/price-alerts", create("rate-b", "80000001"), "80000001"); rr.Code != http.StatusCreated {
		t.Fatalf("second create code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodPatch, "/price-alerts/al_does_not_exist", `{"revision":1}`, "80000001"); rr.Code != http.StatusNotFound {
		t.Fatalf("third write code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr := do(http.MethodDelete, "/price-alerts/al_does_not_exist", "", "80000001")
	if rr.Code != http.StatusTooManyRequests || !strings.Contains(rr.Body.String(), `"code":"RATE_LIMITED"`) || !strings.Contains(rr.Body.String(), `"retryAfterSec":`) || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limit response code=%d headers=%v body=%s", rr.Code, rr.Header(), rr.Body.String())
	}
	if rr := do(http.MethodPost, "/price-alerts", create("isolated", "80000002"), "80000002"); rr.Code != http.StatusCreated {
		t.Fatalf("quota must be per user: code=%d body=%s", rr.Code, rr.Body.String())
	}
	// A new Server (service restart/replica) uses the same durable counter.
	srv = newTestServer(store, m, &fakeNotifier{})
	srv.cfg.AlertWriteLimit = 3
	if rr := do(http.MethodDelete, "/price-alerts/al_does_not_exist", "", "80000001"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("restart bypassed persisted bucket: code=%d body=%s", rr.Code, rr.Body.String())
	}
	// A fresh window resets the durable bucket through the same store gate.
	before := time.Now().Add(2 * time.Minute)
	if err := store.AdmitAlertWrite(context.Background(), "80000001", before, 3, time.Minute); err != nil {
		t.Fatalf("new window not admitted: %v", err)
	}
	if err := store.DeleteAlert(context.Background(), "80000001", "al_does_not_exist", nil, before); apiCode(err) != "ALERT_NOT_FOUND" {
		t.Fatalf("sanity delete: %v", err)
	}
}

func TestAlertEvaluationContinuesPastFirstPage(t *testing.T) {
	store := migratedStore(t)
	m := testMarket("95000.00")
	srv := newTestServer(store, m, &fakeNotifier{})
	ctx := context.Background()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO price_alerts (alert_id,user_id,client_alert_id,request_hash,market,symbol,condition,threshold,enabled,status,created_at,updated_at) VALUES ($1,$2,$3,'bulk-fixture','spot','BTCUSDT','gte',$4,true,'active',now(),now())`)
	if err != nil {
		t.Fatal(err)
	}
	// Eleven users each use the normal 50-active quota. Only the very last ID
	// crosses, proving that pagination does not stop after the first 500 rows.
	lastID := ""
	for i := 0; i < 550; i++ {
		id := fmt.Sprintf("al_page_%04d", i)
		uid := fmt.Sprintf("page-user-%02d", i/50)
		threshold := "100000"
		if i == 549 {
			threshold, lastID = "90000", id
		}
		if _, err := stmt.Exec(id, uid, "bulk-"+id, threshold); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := srv.evaluateSymbol(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	var baselines, triggers, outbox int
	if err := store.db.QueryRow(`SELECT count(*) FROM price_alerts WHERE last_evaluated_at IS NOT NULL`).Scan(&baselines); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM price_alert_trigger_history WHERE alert_id=$1`, lastID).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM price_alert_outbox WHERE alert_id=$1`, lastID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if baselines != 550 || triggers != 1 || outbox != 1 {
		t.Fatalf("evaluated=%d last-page triggers=%d outbox=%d", baselines, triggers, outbox)
	}
	if err := srv.evaluateSymbol(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM price_alert_trigger_history`).Scan(&triggers); err != nil || triggers != 1 {
		t.Fatalf("duplicate trigger after rescan=%d err=%v", triggers, err)
	}
	if lastID <= "al_page_0499" {
		t.Fatal("test fixture did not place crossing beyond first page")
	}
}

func TestAlertWriteLimitConcurrentAcrossServerInstances(t *testing.T) {
	store := migratedStore(t)
	const workers = 12
	const limit = 3
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			uid := "80000001"
			err := store.AdmitAlertWrite(context.Background(), uid, time.Now(), limit, time.Minute)
			if err == nil {
				results <- nil
				return
			}
			var limited *alertWriteRateLimit
			if !errors.As(err, &limited) || limited.retryAfterSec < 1 {
				results <- fmt.Errorf("unexpected limit result: %w", err)
				return
			}
			results <- nil
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := store.db.QueryRow(`SELECT request_count FROM price_alert_write_rate_limits WHERE user_id='80000001'`).Scan(&count); err != nil || count != limit {
		t.Fatalf("concurrent shared bucket count=%d err=%v, want %d", count, err, limit)
	}
}
