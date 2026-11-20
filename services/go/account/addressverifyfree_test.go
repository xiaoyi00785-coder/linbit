package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func doInternalGET(h http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("X-Internal-Token", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// TestAddressVerifyFree exercises the per-address verify-free mark end-to-end:
// mark, read back, internal check, clear, and the 30-day expiry sweep.
func TestAddressVerifyFree(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	target := strings.ToLower(email)

	// Create an address first.
	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	addr := map[string]any{
		"asset": "USDT", "network": "TRC20", "address": "TVFreeAddr0001", "label": "hot",
		"code": srv.recorder.Last(target, purposeAddress),
	}
	w := doJSON(t, h, "POST", "/account/withdraw-addresses", tok.AccessToken, addr)
	if w.Code != http.StatusCreated {
		t.Fatalf("add address = %d %s", w.Code, w.Body.String())
	}
	var created WithdrawAddress
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Mark permanent, needs a fresh code.
	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	body := map[string]string{"mode": verifyFreePermanent, "code": srv.recorder.Last(target, purposeAddress)}
	w = doJSON(t, h, "POST", "/account/withdraw-addresses/"+created.ID+"/verify-free", tok.AccessToken, body)
	if w.Code != http.StatusOK {
		t.Fatalf("set permanent = %d %s", w.Code, w.Body.String())
	}
	var a WithdrawAddress
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if a.VerifyFreeMode != verifyFreePermanent || a.VerifyFreeUntil != nil {
		t.Fatalf("permanent = %+v", a)
	}

	// Without a code the next mark is rejected.
	w = doJSON(t, h, "POST", "/account/withdraw-addresses/"+created.ID+"/verify-free", tok.AccessToken,
		map[string]string{"mode": verifyFreeNone})
	if w.Code == http.StatusOK {
		t.Fatalf("set without code = %d, want sensitive-auth failure", w.Code)
	}

	// Internal /internal/withdraw/verify-free matches the stored mark.
	u, err := srv.store.UserByEmail(t.Context(), target)
	if err != nil || u == nil {
		t.Fatalf("lookup user: %v", err)
	}
	q := "?uid=" + u.UID + "&asset=USDT&network=TRC20&address=TVFreeAddr0001"
	rec := doInternalGET(h, "/internal/withdraw/verify-free"+q, srv.cfg.InternalToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal verify-free = %d %s", rec.Code, rec.Body.String())
	}
	var vf struct {
		VerifyFree bool `json:"verifyFree"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &vf)
	if !vf.VerifyFree {
		t.Fatal("internal verify-free = false, want true")
	}

	// A different address is not verify-free even for the same user.
	q2 := "?uid=" + u.UID + "&asset=USDT&network=TRC20&address=TOtherAddr"
	rec = doInternalGET(h, "/internal/withdraw/verify-free"+q2, srv.cfg.InternalToken)
	_ = json.Unmarshal(rec.Body.Bytes(), &vf)
	if vf.VerifyFree {
		t.Fatal("other address verify-free = true, want false")
	}

	// 30-day mode sets an expiry.
	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	w = doJSON(t, h, "POST", "/account/withdraw-addresses/"+created.ID+"/verify-free", tok.AccessToken,
		map[string]string{"mode": verifyFree30Days, "code": srv.recorder.Last(target, purposeAddress)})
	if w.Code != http.StatusOK {
		t.Fatalf("set 30_days = %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if a.VerifyFreeMode != verifyFree30Days || a.VerifyFreeUntil == nil || a.VerifyFreeUntil.Before(time.Now()) {
		t.Fatalf("30 days = %+v", a)
	}

	// ExpireVerifyFree 1 hour from now leaves the 30-day mark alone.
	if n, err := srv.store.ExpireVerifyFree(t.Context(), time.Now().Add(1*time.Hour)); err != nil || n != 0 {
		t.Fatalf("early expire n=%d err=%v, want 0/nil", n, err)
	}
	// 31 days from now expires it.
	if n, err := srv.store.ExpireVerifyFree(t.Context(), time.Now().Add(31*24*time.Hour)); err != nil || n < 1 {
		t.Fatalf("late expire n=%d err=%v, want >=1/nil", n, err)
	}
	// Internal check now reports false.
	rec = doInternalGET(h, "/internal/withdraw/verify-free"+q, srv.cfg.InternalToken)
	_ = json.Unmarshal(rec.Body.Bytes(), &vf)
	if vf.VerifyFree {
		t.Fatal("after expire verify-free = true, want false")
	}
}

// TestAddressVerifyFreeRejectsUnknownMode confirms the handler validates mode.
func TestAddressVerifyFreeRejectsUnknownMode(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	_ = doJSON(t, h, "POST", "/account/code/send", tok.AccessToken, map[string]string{"purpose": purposeAddress})
	addr := map[string]any{
		"asset": "USDT", "network": "TRC20", "address": "TBadMode", "label": "x",
		"code": srv.recorder.Last(strings.ToLower(email), purposeAddress),
	}
	w := doJSON(t, h, "POST", "/account/withdraw-addresses", tok.AccessToken, addr)
	var created WithdrawAddress
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	w = doJSON(t, h, "POST", "/account/withdraw-addresses/"+created.ID+"/verify-free", tok.AccessToken,
		map[string]string{"mode": "forever"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad mode = %d, want 400", w.Code)
	}
}
