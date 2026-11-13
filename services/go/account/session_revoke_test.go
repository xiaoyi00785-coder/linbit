package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// A5: freezing an account, resetting its password and kicking a device must
// stop live access tokens immediately — not only refresh tokens.

func withRedis(t *testing.T, srv *Server) {
	t.Helper()
	srv.rdb = redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
}

func httptestRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}

// loginNewDevice logs a phone account in on a new device (SMS challenge) and
// returns the tokens bound to that device.
func loginNewDevice(t *testing.T, srv *Server, h http.Handler, phone, deviceID string) authTokens {
	t.Helper()
	w := doJSONDevice(t, h, "POST", "/auth/login", "", deviceID,
		map[string]string{"phone": phone, "password": "Str0ng-Passw0rd!", "deviceId": deviceID, "deviceName": deviceID})
	var ch loginChallengeResponse
	decode(t, w.Body.Bytes(), &ch)
	if ch.ChallengeToken == "" {
		t.Fatalf("login %s: no challenge (%d %s)", deviceID, w.Code, w.Body.String())
	}
	code := srv.recorder.Last(phone, purposeLogin)
	w = doJSON(t, h, "POST", "/auth/login/verify", "", map[string]string{"challengeToken": ch.ChallengeToken, "code": code})
	if w.Code != http.StatusOK {
		t.Fatalf("verify %s = %d %s", deviceID, w.Code, w.Body.String())
	}
	var tok authTokens
	decode(t, w.Body.Bytes(), &tok)
	return tok
}

func TestFreezeRevokesAccessToken(t *testing.T) {
	srv := newTestServer(t)
	withRedis(t, srv)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	if w := doJSON(t, h, "GET", "/account", tok.AccessToken, nil); w.Code != http.StatusOK {
		t.Fatalf("account before freeze = %d", w.Code)
	}
	u, err := srv.store.UserByEmail(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}

	r := httptestRequest(t, "POST", "/internal/admin/users/"+u.UID+"/status", map[string]string{"status": userFrozen, "reason": "test"})
	r.Header.Set("X-Internal-Token", srv.cfg.InternalToken)
	if w := serve(h, r); w.Code != http.StatusOK {
		t.Fatalf("freeze = %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "GET", "/account", tok.AccessToken, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("access token after freeze = %d, want 401", w.Code)
	}
}

func TestPasswordResetRevokesAccessToken(t *testing.T) {
	srv := newTestServer(t)
	withRedis(t, srv)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	_ = doJSON(t, h, "POST", "/auth/code/send", "", map[string]string{"purpose": purposeReset, "email": email})
	code := srv.recorder.Last(strings.ToLower(email), purposeReset)
	vw := doJSON(t, h, "POST", "/auth/password/verify-code", "", map[string]string{"email": email, "code": code})
	if vw.Code != http.StatusOK {
		t.Fatalf("verify-code = %d %s", vw.Code, vw.Body.String())
	}
	var vr verifyResetCodeResponse
	decode(t, vw.Body.Bytes(), &vr)
	if w := doJSON(t, h, "POST", "/auth/password/reset", "", map[string]string{"resetToken": vr.ResetToken, "newPassword": "N3w-Passw0rd!"}); w.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "GET", "/account", tok.AccessToken, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("access token after reset = %d, want 401", w.Code)
	}
}

func TestRemoveDeviceRevokesOnlyThatDevice(t *testing.T) {
	srv := newTestServer(t)
	withRedis(t, srv)
	h := srv.Routes()
	phone := uniquePhone()
	registerWithCode(t, srv, "", phone, "86")

	// Log in on device-A (new device → SMS challenge) and keep its tokens.
	tokA := loginNewDevice(t, srv, h, phone, "device-A")
	tokB := loginNewDevice(t, srv, h, phone, "device-B")

	if w := doJSON(t, h, "DELETE", "/account/devices/cid:device-A", tokB.AccessToken, nil); w.Code != http.StatusNoContent {
		t.Fatalf("remove device-A = %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "GET", "/account", tokA.AccessToken, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("device-A access token after removal = %d, want 401", w.Code)
	}
	if w := doJSON(t, h, "GET", "/account", tokB.AccessToken, nil); w.Code != http.StatusOK {
		t.Fatalf("device-B access token after removing device-A = %d, want 200", w.Code)
	}
}
