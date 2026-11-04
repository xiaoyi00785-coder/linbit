package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// r5: guest GET /spot/fee-schedule passes without a token (even with a stale
// one), and PUT/PATCH amend of futures plan / TP-SL orders is proxied to the
// futures service with the path intact.
func TestR5PublicFeeScheduleAndAlgoAmendRouting(t *testing.T) {
	var got []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path+" uid="+r.Header.Get("X-User-Id"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer up.Close()
	g, err := NewGateway(Config{JWTSecret: "test-secret", OrderURL: up.URL, FuturesURL: up.URL})
	if err != nil {
		t.Fatal(err)
	}
	h := g.Handler()
	for _, auth := range []string{"", "Bearer stale.invalid.token"} {
		req := httptest.NewRequest(http.MethodGet, "/spot/fee-schedule", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("guest fee-schedule (auth=%q) code=%d body=%s", auth, rec.Code, rec.Body.String())
		}
	}
	tok, err := common.SignAccessToken("test-secret", "80000004", "t@x.io", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"PUT", "/futures/plan-orders/12"}, {"PATCH", "/futures/plan-orders/12"},
		{"PUT", "/futures/tpsl-orders/34"}, {"PATCH", "/futures/tpsl-orders/34"}} {
		req := httptest.NewRequest(c[0], c[1], nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s code=%d body=%s", c[0], c[1], rec.Code, rec.Body.String())
		}
		if want := c[0] + " " + c[1] + " uid=80000004"; got[len(got)-1] != want {
			t.Fatalf("upstream saw %q want %q", got[len(got)-1], want)
		}
	}
}
