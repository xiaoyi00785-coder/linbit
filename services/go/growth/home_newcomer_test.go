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
}
