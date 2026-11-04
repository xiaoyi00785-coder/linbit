package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

func TestSupportRoutesToAccountWithAuth(t *testing.T) {
	acct := newUpstream()
	defer acct.server.Close()
	other := newUpstream()
	defer other.server.Close()
	h := newTestGateway(t, acct.server.URL, other.server.URL)

	r := httptest.NewRequest("GET", "/support/tickets", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || acct.hits != 0 {
		t.Fatalf("unauthenticated = %d hits=%d", w.Code, acct.hits)
	}
	token, _ := common.SignAccessToken("test-secret", "80000001", "a@b.c", time.Hour, time.Now())
	r = httptest.NewRequest("GET", "/support/tickets/12", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || acct.lastPath != "/support/tickets/12" || acct.lastUserID != "80000001" {
		t.Fatalf("authed = %d path=%q uid=%q", w.Code, acct.lastPath, acct.lastUserID)
	}
}
