package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// mockU2FCred builds a webauthn.Credential that is well-formed enough to
// round-trip through insertU2F / listU2F. Real end-to-end sign + verify is
// exercised in the browser; this covers our own store, list, delete,
// counter-bump and dedup against passkeys.
func mockU2FCred(id string) *webauthn.Credential {
	return &webauthn.Credential{
		ID:              []byte(id),
		PublicKey:       []byte("fake-pubkey"),
		AttestationType: "none",
		Authenticator:   webauthn.Authenticator{SignCount: 1},
	}
}

func TestU2FStoreCRUD(t *testing.T) {
	srv := newTestServer(t)
	email := uniqueEmail()
	// register through the server so a user row exists
	_ = registerWithCode(t, srv, email, "", "")
	u, err := srv.store.UserByEmail(t.Context(), email)
	if err != nil || u == nil {
		t.Fatalf("lookup user: %v", err)
	}

	c1 := mockU2FCred("yk-1")
	id1, err := srv.store.insertU2F(t.Context(), u.ID, "YubiKey 5", c1)
	if err != nil || id1 == 0 {
		t.Fatalf("insert1: %v id=%d", err, id1)
	}

	// Count and list.
	if n, _ := srv.store.countU2F(t.Context(), u.ID); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	rows, err := srv.store.listU2F(t.Context(), u.ID)
	if err != nil || len(rows) != 1 || rows[0].Name != "YubiKey 5" {
		t.Fatalf("list = %+v err=%v", rows, err)
	}
	if rows[0].CredID != base64.RawURLEncoding.EncodeToString(c1.ID) {
		t.Fatalf("cred id = %s", rows[0].CredID)
	}

	// Inserting the same credential id again returns errU2FExists.
	if _, err := srv.store.insertU2F(t.Context(), u.ID, "dup", c1); err != errU2FExists {
		t.Fatalf("dup insert err = %v, want errU2FExists", err)
	}

	// Counter bump + last_used_at move forward.
	c1.Authenticator.SignCount = 42
	srv.store.updateU2FUse(t.Context(), u.ID, c1)
	rows, _ = srv.store.listU2F(t.Context(), u.ID)
	if rows[0].Cred.Authenticator.SignCount != 42 {
		t.Fatalf("counter = %d, want 42", rows[0].Cred.Authenticator.SignCount)
	}
	if !rows[0].LastUsedAt.Valid || time.Since(rows[0].LastUsedAt.Time) > time.Minute {
		t.Fatalf("last_used_at not bumped: %+v", rows[0].LastUsedAt)
	}

	// Delete.
	ok, err := srv.store.deleteU2F(t.Context(), u.ID, "" /* bogus */)
	if err != nil || ok {
		t.Fatalf("delete bogus = %v err=%v, want false", ok, err)
	}
	// Real id:
	idStr := rows[0].idStr()
	ok, err = srv.store.deleteU2F(t.Context(), u.ID, idStr)
	if err != nil || !ok {
		t.Fatalf("delete = %v err=%v, want true", ok, err)
	}
	if n, _ := srv.store.countU2F(t.Context(), u.ID); n != 0 {
		t.Fatalf("count after delete = %d, want 0", n)
	}
}

// TestU2FHandlerAuthRequired checks the authed endpoints reject missing tokens
// and that listing requires auth — the shape of the public contract.
func TestU2FHandlerAuthRequired(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()

	w := doJSON(t, h, "GET", "/account/security/u2f/keys", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("list without token = %d, want 401", w.Code)
	}

	tok := registerWithCode(t, srv, uniqueEmail(), "", "")
	// Without a configured RP id the handler answers 503 PASSKEY_UNAVAILABLE
	// before the sensitive-auth check runs — that is the test-environment
	// default, and the right shape of answer for a disabled feature.
	w = doJSON(t, h, "POST", "/account/security/u2f/register/begin", tok.AccessToken, map[string]string{})
	if srv.cfg.WebAuthnRPID == "" {
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("begin without RP id = %d, want 503", w.Code)
		}
	} else if w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
		t.Fatalf("begin without 2FA = %d, want 400/403", w.Code)
	}

	// List for an authed user is OK and returns empty items + enabled flag.
	w = doJSON(t, h, "GET", "/account/security/u2f/keys", tok.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list authed = %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Items   []u2fDTO `json:"items"`
		Enabled bool     `json:"enabled"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 0 {
		t.Fatalf("empty list items = %d, want 0", len(list.Items))
	}
	if list.Enabled != (srv.cfg.WebAuthnRPID != "") {
		t.Fatalf("enabled flag = %v, want %v", list.Enabled, srv.cfg.WebAuthnRPID != "")
	}
}

// Helper on u2fRow so tests can get the row's id as the DELETE path wants it.
func (r u2fRow) idStr() string {
	return itoa64(r.ID)
}

func itoa64(n int64) string { // avoid pulling strconv into test for a one-liner
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(buf[i:])
	if neg {
		s = "-" + s
	}
	return s
}
