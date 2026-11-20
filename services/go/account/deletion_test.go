package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// deletion_test.go: /account/delete-* lifecycle.
//
// We stub the asset-balance probe with a tiny httptest server; the real asset
// service isn't needed for these tests.

func newBalanceStub(t *testing.T, items string) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":` + items + `}`))
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func sendSecurityCode(t *testing.T, srv *Server, h http.Handler, token, target string) string {
	t.Helper()
	// POST /account/code/send with purpose=security sends a code to the user's own contact.
	w := doJSON(t, h, "POST", "/account/code/send", token, map[string]string{"purpose": purposeSecurity})
	if w.Code != http.StatusOK {
		t.Fatalf("send security code: %d %s", w.Code, w.Body.String())
	}
	code := srv.recorder.Last(target, purposeSecurity)
	if code == "" {
		t.Fatalf("no security code recorded for %s", target)
	}
	return code
}

func TestAccountDeleteRequest_SevenDayGraceAndCancel(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.AssetInternalURL = newBalanceStub(t, "[]") // empty balances
	srv.cfg.InternalToken = "test-internal"
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	code := sendSecurityCode(t, srv, h, tok.AccessToken, strings.ToLower(email))
	w := doJSON(t, h, "POST", "/account/delete-request", tok.AccessToken, map[string]string{"code": code})
	if w.Code != http.StatusCreated {
		t.Fatalf("delete-request = %d %s", w.Code, w.Body.String())
	}
	var resp deleteStatusResponse
	decode(t, w.Body.Bytes(), &resp)
	if !resp.Pending || resp.ScheduledAt == nil || !resp.Cancellable {
		t.Fatalf("unexpected delete status %+v", resp)
	}
	// scheduled_at must be ~7 days out.
	sched, err := time.Parse(time.RFC3339, *resp.ScheduledAt)
	if err != nil {
		t.Fatalf("parse scheduledAt: %v", err)
	}
	if diff := time.Until(sched); diff < 6*24*time.Hour || diff > 8*24*time.Hour {
		t.Fatalf("scheduledAt not 7d out: %v", diff)
	}

	// GET status matches.
	w = doJSON(t, h, "GET", "/account/delete-status", tok.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete-status = %d", w.Code)
	}
	decode(t, w.Body.Bytes(), &resp)
	if !resp.Pending {
		t.Fatalf("status not pending: %+v", resp)
	}

	// Cancel during the window works; subsequent status reports no pending.
	w = doJSON(t, h, "POST", "/account/delete-cancel", tok.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete-cancel = %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, h, "GET", "/account/delete-status", tok.AccessToken, nil)
	decode(t, w.Body.Bytes(), &resp)
	if resp.Pending {
		t.Fatalf("still pending after cancel: %+v", resp)
	}
	// Re-cancel is a 404 (nothing to cancel).
	w = doJSON(t, h, "POST", "/account/delete-cancel", tok.AccessToken, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second cancel = %d, want 404", w.Code)
	}
}

func TestAccountDeleteRequest_RejectsNonZeroBalance(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.AssetInternalURL = newBalanceStub(t, `[{"asset":"USDT","free":"12.5","locked":"0","total":"12.5"}]`)
	srv.cfg.InternalToken = "test-internal"
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	code := sendSecurityCode(t, srv, h, tok.AccessToken, strings.ToLower(email))
	w := doJSON(t, h, "POST", "/account/delete-request", tok.AccessToken, map[string]string{"code": code})
	if w.Code != http.StatusConflict {
		t.Fatalf("delete-request with balance = %d, want 409", w.Code)
	}
	var out map[string]any
	decode(t, w.Body.Bytes(), &out)
	if out["code"] != "BALANCE_NOT_ZERO" {
		t.Fatalf("unexpected body %v", out)
	}
	// No pending request should exist.
	w = doJSON(t, h, "GET", "/account/delete-status", tok.AccessToken, nil)
	var st deleteStatusResponse
	decode(t, w.Body.Bytes(), &st)
	if st.Pending {
		t.Fatalf("pending created despite 409: %+v", st)
	}
}

func TestDeletionSweeper_OnlyCompletesWhenDue(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.AssetInternalURL = newBalanceStub(t, "[]")
	srv.cfg.InternalToken = "test-internal"
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	// Resolve the user's uid up front; we advance the clock later which will
	// expire the access token signature-wise.
	claims, err := common.ParseAccessToken(srv.cfg.JWTSecret, tok.AccessToken)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	uid := claims.Subject

	code := sendSecurityCode(t, srv, h, tok.AccessToken, strings.ToLower(email))
	if w := doJSON(t, h, "POST", "/account/delete-request", tok.AccessToken, map[string]string{"code": code}); w.Code != http.StatusCreated {
		t.Fatalf("delete-request = %d %s", w.Code, w.Body.String())
	}
	// Sweep with the real clock: nothing is due yet.
	srv.sweepDeletions(t.Context())
	w := doJSON(t, h, "GET", "/account/delete-status", tok.AccessToken, nil)
	var st deleteStatusResponse
	decode(t, w.Body.Bytes(), &st)
	if !st.Pending {
		t.Fatalf("sweeper fired before grace period: %+v", st)
	}

	// Advance the clock past the grace period and re-sweep.
	srv.now = func() time.Time { return time.Now().Add(deletionGracePeriod + time.Minute) }
	srv.sweepDeletions(t.Context())

	// Scrub applied: original email no longer resolves, uid still does but with placeholder.
	if _, err := srv.store.UserByEmail(t.Context(), strings.ToLower(email)); err == nil {
		t.Fatalf("original email still resolves after sweep")
	}
	u, err := srv.store.UserByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("load user after sweep: %v", err)
	}
	if u.Status != "deleted" {
		t.Fatalf("user status = %q, want deleted", u.Status)
	}
	if !strings.Contains(u.Email.String, "@removed.invalid") {
		t.Fatalf("email not scrubbed: %q", u.Email.String)
	}
}
