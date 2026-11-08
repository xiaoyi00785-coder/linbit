package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/pquerna/otp/totp"
)

func TestUserAPIKeyPersistence(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Routes()
	alice := registerWithCode(t, srv, uniqueEmail(), "", "")
	bob := registerWithCode(t, srv, uniqueEmail(), "", "")
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Linbit", AccountName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := common.ParseAccessToken(srv.cfg.JWTSecret, alice.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.db.Exec(`UPDATE users SET totp_secret=$2, totp_enabled=TRUE WHERE uid=$1`, claims.Subject, key.Secret()); err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bad := doJSON(t, h, "POST", "/account/api-keys", alice.AccessToken, map[string]any{"label": "手机", "permissions": []string{"spot_trade"}, "totpCode": code})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("trade perm %d %s", bad.Code, bad.Body.String())
	}
	no2 := doJSON(t, h, "POST", "/account/api-keys", alice.AccessToken, map[string]any{"label": "手机", "permissions": []string{"read"}})
	if no2.Code != http.StatusForbidden {
		t.Fatalf("missing totp %d %s", no2.Code, no2.Body.String())
	}
	created := doJSON(t, h, "POST", "/account/api-keys", alice.AccessToken, map[string]any{
		"label": "手机", "permissions": []string{"read"}, "ipWhitelist": []string{"203.0.113.5"}, "totpCode": code,
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create %d %s", created.Code, created.Body.String())
	}
	var out struct {
		Item   userKeyDTO `json:"item"`
		Secret string     `json:"secret"`
	}
	if json.Unmarshal(created.Body.Bytes(), &out) != nil || out.Secret == "" || !strings.HasPrefix(out.Item.APIKey, "lbu_") {
		t.Fatalf("created %s", created.Body.String())
	}
	listed := doJSON(t, h, "GET", "/account/api-keys", alice.AccessToken, nil)
	if strings.Contains(listed.Body.String(), out.Secret) || !strings.Contains(listed.Body.String(), out.Item.APIKey) {
		t.Fatalf("list leaked or missing key: %s", listed.Body.String())
	}
	if w := doJSON(t, h, "DELETE", "/account/api-keys/"+out.Item.ID, bob.AccessToken, nil); w.Code != http.StatusNotFound {
		t.Fatalf("bob delete %d %s", w.Code, w.Body.String())
	}
	ver := internalReq(t, h, "POST", "/internal/api-keys/verify", map[string]string{"apiKey": out.Item.APIKey, "apiSecret": out.Secret, "ip": "203.0.113.5"})
	if ver.Code != 200 || !strings.Contains(ver.Body.String(), `"ok":true`) || !strings.Contains(ver.Body.String(), `"permissions":["read"]`) {
		t.Fatalf("verify %d %s", ver.Code, ver.Body.String())
	}
	denied := internalReq(t, h, "POST", "/internal/api-keys/verify", map[string]string{"apiKey": out.Item.APIKey, "apiSecret": out.Secret, "ip": "198.51.100.9"})
	if !strings.Contains(denied.Body.String(), "ip_not_allowed") {
		t.Fatalf("ip %s", denied.Body.String())
	}
	if w := doJSON(t, h, "DELETE", "/account/api-keys/"+out.Item.ID, alice.AccessToken, nil); w.Code != 200 {
		t.Fatalf("revoke %d %s", w.Code, w.Body.String())
	}
	after := internalReq(t, h, "POST", "/internal/api-keys/verify", map[string]string{"apiKey": out.Item.APIKey, "apiSecret": out.Secret, "ip": "203.0.113.5"})
	if strings.Contains(after.Body.String(), `"ok":true`) {
		t.Fatalf("revoked still valid %s", after.Body.String())
	}
}
