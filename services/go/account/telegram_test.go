package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// telegram_test.go: TG Login Widget signature verification and the bind/unbind
// handler lifecycle.

const testTGToken = "123456:ABC-TESTTOKEN"

// signTelegram reproduces what the Login Widget does: HMAC_SHA256 of the
// alphabetically sorted "k=v" pairs, using SHA256(botToken) as the key.
func signTelegram(t *testing.T, botToken string, pairs map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(pairs[k])
	}
	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

func tgBindPayload(t *testing.T, id int64, username string, authDate int64) map[string]any {
	t.Helper()
	// Matches the fields verifyTelegramLogin includes in the data_check_string.
	pairs := map[string]string{
		"id":        strconv.FormatInt(id, 10),
		"auth_date": strconv.FormatInt(authDate, 10),
	}
	if username != "" {
		pairs["username"] = username
	}
	hash := signTelegram(t, testTGToken, pairs)
	return map[string]any{
		"telegramId":       id,
		"telegramUsername": username,
		"authDate":         authDate,
		"authHash":         hash,
	}
}

func TestVerifyTelegramLogin_GoodAndBadSignature(t *testing.T) {
	now := time.Now()
	req := bindTelegramReq{
		ID:       42,
		Username: "sea_gull",
		AuthDate: now.Unix(),
		Hash:     "", // filled below
	}
	pairs := map[string]string{
		"id":        strconv.FormatInt(req.ID, 10),
		"username":  req.Username,
		"auth_date": strconv.FormatInt(req.AuthDate, 10),
	}
	req.Hash = signTelegram(t, testTGToken, pairs)
	if !verifyTelegramLogin(testTGToken, req, now, maxTelegramAuthAge) {
		t.Fatalf("good signature rejected")
	}
	// Flip one byte in the hash: rejected.
	bad := req
	bad.Hash = strings.Repeat("0", len(req.Hash))
	if verifyTelegramLogin(testTGToken, bad, now, maxTelegramAuthAge) {
		t.Fatalf("bad signature accepted")
	}
	// Wrong token: rejected.
	if verifyTelegramLogin("other:token", req, now, maxTelegramAuthAge) {
		t.Fatalf("signature from wrong token accepted")
	}
	// Stale auth_date: rejected.
	stale := req
	stale.AuthDate = now.Add(-48 * time.Hour).Unix()
	stale.Hash = signTelegram(t, testTGToken, map[string]string{
		"id":        strconv.FormatInt(stale.ID, 10),
		"username":  stale.Username,
		"auth_date": strconv.FormatInt(stale.AuthDate, 10),
	})
	if verifyTelegramLogin(testTGToken, stale, now, maxTelegramAuthAge) {
		t.Fatalf("stale auth_date accepted")
	}
}

func TestBindTelegram_UnavailableWithoutToken(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	srv.cfg.TelegramBotToken = ""
	body := tgBindPayload(t, 1001, "alice", time.Now().Unix())
	w := doJSON(t, h, "POST", "/account/bind-telegram", tok.AccessToken, body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("bind without token = %d, want 503", w.Code)
	}
}

func TestBindTelegram_RejectsBadSignature(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.TelegramBotToken = testTGToken
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")
	w := doJSON(t, h, "POST", "/account/bind-telegram", tok.AccessToken, map[string]any{
		"telegramId": 1234, "authDate": time.Now().Unix(),
		"authHash": strings.Repeat("a", 64),
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad sig = %d, want 401", w.Code)
	}
}

func TestBindTelegram_HappyPathAndUnbind(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.TelegramBotToken = testTGToken
	h := srv.Routes()
	email := uniqueEmail()
	tok := registerWithCode(t, srv, email, "", "")

	// initial status: not bound, service available
	w := doJSON(t, h, "GET", "/account/telegram", tok.AccessToken, nil)
	var st telegramStatus
	decode(t, w.Body.Bytes(), &st)
	if st.Bound || !st.Available {
		t.Fatalf("initial status %+v", st)
	}

	// Bind with a real signature.
	tgID := int64(555000111)
	body := tgBindPayload(t, tgID, "haiou", time.Now().Unix())
	w = doJSON(t, h, "POST", "/account/bind-telegram", tok.AccessToken, body)
	if w.Code != http.StatusOK {
		t.Fatalf("bind = %d %s", w.Code, w.Body.String())
	}
	decode(t, w.Body.Bytes(), &st)
	if !st.Bound || st.ID == nil || *st.ID != tgID || st.Username == nil || *st.Username != "haiou" {
		t.Fatalf("post-bind status %+v", st)
	}

	// A second user cannot bind the same TG id (unique index).
	email2 := uniqueEmail()
	tok2 := registerWithCode(t, srv, email2, "", "")
	body2 := tgBindPayload(t, tgID, "haiou", time.Now().Unix())
	w = doJSON(t, h, "POST", "/account/bind-telegram", tok2.AccessToken, body2)
	if w.Code != http.StatusConflict {
		t.Fatalf("second bind of same tg id = %d, want 409", w.Code)
	}

	// Unbind restores the empty state.
	w = doJSON(t, h, "POST", "/account/unbind-telegram", tok.AccessToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("unbind = %d", w.Code)
	}
	w = doJSON(t, h, "GET", "/account/telegram", tok.AccessToken, nil)
	decode(t, w.Body.Bytes(), &st)
	if st.Bound {
		t.Fatalf("still bound after unbind: %+v", st)
	}

	// After unbind the other user can grab the id.
	body3 := tgBindPayload(t, tgID, "haiou", time.Now().Unix())
	w = doJSON(t, h, "POST", "/account/bind-telegram", tok2.AccessToken, body3)
	if w.Code != http.StatusOK {
		t.Fatalf("rebind to second user = %d %s", w.Code, w.Body.String())
	}
}

// Sanity: verifyTelegramLogin rejects a payload missing required fields.
func TestVerifyTelegramLogin_MissingFields(t *testing.T) {
	cases := []bindTelegramReq{
		{Hash: "deadbeef", AuthDate: time.Now().Unix()},               // no id
		{ID: 1, AuthDate: time.Now().Unix()},                          // no hash
		{ID: 1, Hash: "deadbeef"},                                     // no auth_date
		{ID: 1, Hash: "not-hex", AuthDate: time.Now().Unix()},         // malformed hash
		{ID: 1, Hash: fmt.Sprintf("%064d", 0), AuthDate: 1},           // ancient auth_date
	}
	for i, c := range cases {
		if verifyTelegramLogin(testTGToken, c, time.Now(), maxTelegramAuthAge) {
			t.Fatalf("case %d accepted: %+v", i, c)
		}
	}
}
