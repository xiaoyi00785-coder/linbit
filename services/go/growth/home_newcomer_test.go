package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHomeNewcomerAnonymousAndEmpty(t *testing.T) {
	s := &Server{cfg: Config{Newcomer: loadNewcomerRules(), PayoutAsset: "USDT"}}
	w := httptest.NewRecorder()
	s.handleHomeNewcomer(w, httptest.NewRequest("GET", "/home/newcomer", nil))
	var out struct {
		Available, LoggedIn bool
		Cta                 string
		Tasks               []homeTask
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || w.Code != 200 || !out.Available || out.LoggedIn || out.Cta != "login" || len(out.Tasks) < 3 {
		t.Fatalf("anon: %d %s", w.Code, w.Body)
	}
	assertClaimableJSONString(t, w.Body.Bytes(), "0")
	for _, tk := range out.Tasks {
		if tk.Status != "locked" {
			t.Fatalf("anon task not locked: %+v", tk)
		}
	}
	s.cfg.Newcomer.Tasks = nil
	w = httptest.NewRecorder()
	s.handleHomeNewcomer(w, httptest.NewRequest("GET", "/home/newcomer", nil))
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Available || out.Cta != "none" || len(out.Tasks) != 0 {
		t.Fatalf("empty: %d %s", w.Code, w.Body)
	}
	assertClaimableJSONString(t, w.Body.Bytes(), "0")
}

// claimable is the count of tasks ready to claim, encoded as a JSON string
// (OpenAPI HomeNewcomer.claimable type string). It is not a USDT amount.
func assertClaimableJSONString(t *testing.T, body []byte, want string) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	got := raw["claimable"]
	if len(got) == 0 || got[0] != '"' {
		t.Fatalf("claimable JSON type = %s, want string", got)
	}
	var s string
	if err := json.Unmarshal(got, &s); err != nil || s != want {
		t.Fatalf("claimable = %s, want %q", got, want)
	}
}
