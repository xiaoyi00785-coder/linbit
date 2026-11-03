package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAgeYears(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		dob  string
		want int
	}{
		{"2008-11-03", 18}, // birthday today
		{"2008-11-04", 17}, // birthday tomorrow
		{"1990-01-15", 36},
	}
	for _, c := range cases {
		d, _ := time.Parse("2006-01-02", c.dob)
		if got := ageYears(d, now); got != c.want {
			t.Errorf("ageYears(%s) = %d, want %d", c.dob, got, c.want)
		}
	}
}

func TestParseIDs(t *testing.T) {
	if id, ok := parseDocID("doc_12"); !ok || id != 12 {
		t.Fatalf("parseDocID doc_12 = %d %v", id, ok)
	}
	if _, ok := parseDocID("doc_x"); ok {
		t.Fatal("parseDocID accepted garbage")
	}
	if _, ok := parseAppID("app_0"); ok {
		t.Fatal("parseAppID accepted 0")
	}
	if id, ok := parseAppID("7"); !ok || id != 7 {
		t.Fatalf("parseAppID 7 = %d %v", id, ok)
	}
	if docFileID(3) != "doc_3" || appID(4) != "app_4" {
		t.Fatal("id render mismatch")
	}
}

func TestValidSide(t *testing.T) {
	for _, s := range []string{"front", "selfie", "back"} {
		if !validSide(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	if validSide("left") {
		t.Error("left should be invalid")
	}
}

func TestAdminRequiresInternalToken(t *testing.T) {
	s := &Server{cfg: Config{InternalToken: "tok"}}
	h := s.Routes()
	for _, tok := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodGet, "/admin/kyc/applications", nil)
		if tok != "" {
			req.Header.Set("X-Internal-Token", tok)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("token %q: status %d, want 401", tok, rr.Code)
		}
	}
}

func TestUserEndpointsRequireUser(t *testing.T) {
	s := &Server{cfg: Config{InternalToken: "tok"}}
	h := s.Routes()
	for _, p := range []string{"/kyc/status"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", p, rr.Code)
		}
	}
}
