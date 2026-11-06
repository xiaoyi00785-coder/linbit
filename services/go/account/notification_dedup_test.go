package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPriceAlertInAppNotificationDedup(t *testing.T) {
	srv := newTestServer(t)
	email := uniqueEmail()
	registerWithCode(t, srv, email, "", "")
	u, err := srv.store.UserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	key := "price-alert:old-alert:17"
	var firstID string
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/internal/notifications", strings.NewReader(`{"uid":"`+u.UID+`","kind":"system","title":"BTCUSDT 价格预警","body":"local test only","email":false,"dedupKey":"`+key+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", "test-internal")
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("delivery %d: %s", w.Code, w.Body.String())
		}
		var got struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			t.Cleanup(func() {
				_, _ = srv.store.db.Exec(`DELETE FROM notifications WHERE user_id=$1 AND dedup_key=$2`, u.ID, key)
			})
		}
		if i == 0 {
			firstID = got.ID
		} else if got.ID != firstID {
			t.Fatal("replay identity changed")
		}
	}
	var count int
	if err := srv.store.db.QueryRow(`SELECT count(*) FROM notifications WHERE user_id=$1 AND dedup_key=$2 AND email_status='none'`, u.ID, key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("notifications for one epoch=%d, want 1", count)
	}
}
