package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type captureMail struct {
	subjects []string
	fail     error
}

func (c *captureMail) SendEmailCode(context.Context, string, string, string) error { return c.fail }
func (c *captureMail) SendEmail(_ context.Context, _, subject, _ string) error {
	c.subjects = append(c.subjects, subject)
	return c.fail
}

func TestCodeEmailSubjectIsUnique(t *testing.T) {
	if got := codeEmailSubject("登录验证码", "123456"); got != "Linbit 登录验证码 123456" {
		t.Fatalf("subject %q", got)
	}
	inner := &captureMail{}
	m := antiPhishingMailer{inner: inner}
	_ = m.inner.SendEmail(context.Background(), "a@b.c", codeEmailSubject(purposeSubject[purposeLogin], "654321"), "x")
	if !strings.Contains(inner.subjects[0], "654321") {
		t.Fatal(inner.subjects)
	}
}

func TestRateLimitedHasRetryAfter(t *testing.T) {
	w := httptest.NewRecorder()
	writeSendCodeError(w, "t", ErrRateLimited)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Retry-After"))
	}
	w = httptest.NewRecorder()
	writeSendCodeError(w, "t", &DeliveryError{Channel: channelEmail, Reason: "network"})
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "EMAIL_UNAVAILABLE") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// DB-backed: login step 1 surfaces send failures and rate limits.
func TestLoginSendErrorsSurface(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	registerWithCode(t, srv, email, "", "")
	login := map[string]string{"email": email, "password": "Str0ng-Passw0rd!", "deviceId": "dev-new", "deviceName": "x"}

	srv.codes.email = &captureMail{fail: &DeliveryError{Channel: channelEmail, Reason: "network"}}
	if w := doJSONDevice(t, h, "POST", "/auth/login", "", "dev-new", login); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("send failure: %d %s", w.Code, w.Body.String())
	}
	srv.codes.email = &captureMail{}
	srv.codes.minInterval = time.Hour
	if w := doJSONDevice(t, h, "POST", "/auth/login", "", "dev-new", login); w.Code != http.StatusOK {
		t.Fatalf("first: %d", w.Code)
	}
	w := doJSONDevice(t, h, "POST", "/auth/login", "", "dev-new", login)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limited: %d %s", w.Code, w.Body.String())
	}
	// Wrong password stays 401 (no extra account signal).
	login["password"] = "wrong"
	if w := doJSONDevice(t, h, "POST", "/auth/login", "", "dev-new", login); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad pw: %d", w.Code)
	}
}

// DB-backed: logout kills the access token now; expired access + refresh works.
func TestLogoutRevokesAccessToken(t *testing.T) {
	srv := newTestServer(t)
	srv.rdb = redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	h := srv.Routes()
	tok := registerWithCode(t, srv, uniqueEmail(), "", "")
	if w := doJSON(t, h, "GET", "/account", tok.AccessToken, nil); w.Code != http.StatusOK {
		t.Fatalf("account: %d", w.Code)
	}
	if w := doJSON(t, h, "POST", "/auth/logout", tok.AccessToken, nil); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "GET", "/account", tok.AccessToken, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("old access after logout: %d", w.Code)
	}
	if w := doJSON(t, h, "POST", "/auth/refresh", "", map[string]string{"refreshToken": tok.RefreshToken}); w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: %d", w.Code)
	}

	// Expired access token: refreshToken in the body still logs out.
	tok2 := registerWithCode(t, srv, uniqueEmail(), "", "")
	w := doJSON(t, h, "POST", "/auth/logout", "expired.or.garbage", map[string]string{"refreshToken": tok2.RefreshToken})
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout via refresh: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(t, h, "GET", "/account", tok2.AccessToken, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("access after refresh-logout: %d", w.Code)
	}
	if w := doJSON(t, h, "POST", "/auth/logout", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anon logout: %d", w.Code)
	}
}
