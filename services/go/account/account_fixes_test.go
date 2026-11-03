package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// multipartFile builds a multipart/form-data body with a single file field.
func multipartFile(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// uploadAvatar POSTs a multipart avatar and returns the recorder.
func uploadAvatar(t *testing.T, h http.Handler, token, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartFile(t, avatarFormField, filename, content)
	r := httptest.NewRequest("POST", "/account/avatar", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUploadAvatarRoundtrip(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	tok := registerWithCode(t, srv, uniqueEmail(), "", "")

	// A minimal valid PNG (magic bytes + payload) is accepted and stored.
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("pixeldata")...)
	w := uploadAvatar(t, h, tok.AccessToken, "me.png", png)
	if w.Code != http.StatusOK {
		t.Fatalf("upload png = %d %s", w.Code, w.Body.String())
	}
	var acct accountResponse
	_ = json.Unmarshal(w.Body.Bytes(), &acct)
	if acct.AvatarURL == nil || !strings.HasPrefix(*acct.AvatarURL, "/account/avatars/") {
		t.Fatalf("avatar url = %v, want /account/avatars/ prefix", acct.AvatarURL)
	}

	// The controlled retrieval route serves the bytes back with the right type.
	gr := httptest.NewRequest("GET", *acct.AvatarURL, nil)
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, gr)
	if gw.Code != http.StatusOK {
		t.Fatalf("serve avatar = %d", gw.Code)
	}
	if ct := gw.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("served content-type = %q, want image/png", ct)
	}
	if !bytes.Equal(gw.Body.Bytes(), png) {
		t.Fatal("served avatar bytes differ from uploaded")
	}
}

func TestUploadAvatarRejectsType(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	tok := registerWithCode(t, srv, uniqueEmail(), "", "")

	// A GIF (unsupported) is rejected by content sniffing with 415.
	gif := []byte("GIF89a this is not a supported image")
	if w := uploadAvatar(t, h, tok.AccessToken, "x.gif", gif); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("gif upload = %d, want 415 (%s)", w.Code, w.Body.String())
	}
	// A file whose bytes are plain text (even named .png) is rejected too.
	if w := uploadAvatar(t, h, tok.AccessToken, "fake.png", []byte("hello world")); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("fake png = %d, want 415", w.Code)
	}
}

func TestUploadAvatarRejectsOversize(t *testing.T) {
	srv := newTestServer(t)
	// Shrink the limit so the test payload stays small.
	srv.cfg.AvatarMaxBytes = 1024
	h := srv.Routes()
	tok := registerWithCode(t, srv, uniqueEmail(), "", "")

	big := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte("x"), 4096)...)
	w := uploadAvatar(t, h, tok.AccessToken, "big.png", big)
	// Either the body cap (400) or the explicit size check (413) may fire.
	if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Fatalf("oversize upload = %d, want 413/400 (%s)", w.Code, w.Body.String())
	}
}

func TestDeviceCurrentFlag(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	tok := registerWithCode(t, srv, uniqueEmail(), "", "")

	// The register device has an empty-UA fingerprint; a device-list request with
	// no deviceId/UA shares that fingerprint and is marked current.
	w := doJSON(t, h, "GET", "/account/devices", tok.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list devices = %d %s", w.Code, w.Body.String())
	}
	var dl devicesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &dl)
	if len(dl.Items) == 0 {
		t.Fatal("no devices listed")
	}
	current := 0
	for _, d := range dl.Items {
		if d.Current {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("current device count = %d, want exactly 1", current)
	}

	// A request from an unrecognised device (X-Device-Id) marks nothing current.
	r := httptest.NewRequest("GET", "/account/devices", nil)
	r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	r.Header.Set("X-Device-Id", "totally-unknown-device")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r)
	var dl2 devicesResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &dl2)
	for _, d := range dl2.Items {
		if d.Current {
			t.Fatal("unknown device must not be flagged current")
		}
	}
}

func TestPhoneValidationReturns400(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()

	// send-code: phone without a country code is a clear 400, not a vague error.
	if w := doJSON(t, h, "POST", "/auth/code/send", "",
		map[string]string{"purpose": "register", "phone": "13800138000"}); w.Code != http.StatusBadRequest {
		t.Fatalf("send-code missing country code = %d, want 400", w.Code)
	}
	// register: malformed phone is 400.
	if w := doJSON(t, h, "POST", "/auth/register", "",
		map[string]string{"phone": "13-abc", "countryCode": "86", "password": "Str0ng-Passw0rd!", "code": "123456"}); w.Code != http.StatusBadRequest {
		t.Fatalf("register bad phone = %d, want 400", w.Code)
	}
	// login: phone missing country code must be 400 (previously a misleading 401).
	if w := doJSON(t, h, "POST", "/auth/login", "",
		map[string]string{"phone": "13800138000", "password": "whatever"}); w.Code != http.StatusBadRequest {
		t.Fatalf("login missing country code = %d, want 400", w.Code)
	}
	// login: well-formed handle but unknown account stays 401 (credential error).
	if w := doJSON(t, h, "POST", "/auth/login", "",
		map[string]string{"phone": "13900139000", "countryCode": "86", "password": "whatever"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("login unknown account = %d, want 401", w.Code)
	}
}

// The web client registers and logs in with the same body deviceId; the first
// login from the registering browser must not require a second factor, and
// removing a different device must keep it trusted.
func TestRegisterDeviceIDTrustedOnLogin(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	if w := doJSON(t, h, "POST", "/auth/code/send", "", map[string]string{"purpose": purposeRegister, "email": email}); w.Code != http.StatusOK {
		t.Fatalf("send code: %d", w.Code)
	}
	code := srv.recorder.Last(strings.ToLower(email), purposeRegister)
	pw := "Str0ng-Passw0rd!"
	w := doJSON(t, h, "POST", "/auth/register", "", map[string]string{"email": email, "password": pw, "code": code, "deviceId": "web-reg1", "deviceName": "Chrome on macOS"})
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	var tok authTokens
	_ = json.Unmarshal(w.Body.Bytes(), &tok)
	login := func() *httptest.ResponseRecorder {
		return doJSON(t, h, "POST", "/auth/login", "", map[string]string{"email": email, "password": pw, "deviceId": "web-reg1"})
	}
	var lt authTokens
	if w := login(); w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &lt) != nil || lt.AccessToken == "" {
		t.Fatalf("first login from registering device should skip challenge: %d %s", w.Code, w.Body.String())
	}
	// Device list with X-Device-Id flags exactly that device as current.
	r := httptest.NewRequest("GET", "/account/devices", nil)
	r.Header.Set("Authorization", "Bearer "+lt.AccessToken)
	r.Header.Set("X-Device-Id", "web-reg1")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r)
	var dl devicesResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &dl)
	if len(dl.Items) != 1 || !dl.Items[0].Current || dl.Items[0].DeviceKey != "cid:web-reg1" {
		t.Fatalf("devices = %s, want single current cid:web-reg1", w2.Body.String())
	}
}
