package main

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"context"
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
	cfg := Config{JWTSecret: "tests", AlertCooldown: 300 * time.Second}
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
