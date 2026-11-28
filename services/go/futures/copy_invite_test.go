package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCopyInviteLookup(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1, "follower": 1})
	mux := http.NewServeMux()
	s.copyRoutes(mux)

	get := func(path string) (int, map[string]any) {
		t.Helper()
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		return rr.Code, body
	}

	// no such code
	st, body := get("/futures/copy/invite?code=123456")
	if st != http.StatusBadRequest || body["code"] != "INVITATION_NOT_FOUND" {
		t.Fatalf("missing code: http %d %+v", st, body)
	}

	nick, inv, ident := "Alpha", "123456", "PRIVATE_AREA"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick, InvitationCode: &inv, DealerIdentity: &ident}); err != nil {
		t.Fatal(err)
	}
	// pending application must not be resolvable
	st, body = get("/futures/copy/invite?code=123456")
	if st != http.StatusBadRequest || body["code"] != "INVITATION_NOT_FOUND" {
		t.Fatalf("pending leaked: http %d %+v", st, body)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}
	st, body = get("/futures/copy/invite?code=123456")
	if st != http.StatusOK || body["traderId"] != "trader" || body["dealer_identity"] != "PRIVATE_AREA" {
		t.Fatalf("lookup: http %d %+v", st, body)
	}
	if _, ok := body["invitationCode"]; ok && body["invitationCode"] != "" && body["invitationCode"] != nil {
		t.Fatalf("invite code leaked: %+v", body)
	}
	// case-insensitive (codes are digits here; alphabetic still folds)
	st, body = get("/futures/copy/invite?code=654321")
	if st != http.StatusBadRequest || body["code"] != "INVITATION_NOT_FOUND" {
		t.Fatalf("wrong code: http %d %+v", st, body)
	}
}
