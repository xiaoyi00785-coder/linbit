package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVolumeDays(t *testing.T) {
	for in, want := range map[string]int{"": 30, "1": 1, "30": 30, "365": 365} {
		if got, ok := parseVolumeDays(in); !ok || got != want {
			t.Fatalf("%q -> %d %v", in, got, ok)
		}
	}
	for _, in := range []string{"0", "-1", "366", "abc", "1.5"} {
		if _, ok := parseVolumeDays(in); ok {
			t.Fatalf("%q must be rejected", in)
		}
	}
}

func TestInternalVolumeValidationAndAuth(t *testing.T) {
	s := &Server{cfg: Config{InternalToken: "tok"}}
	h := s.Routes()
	for _, tc := range []struct {
		url, token string
		want       int
	}{
		{"/internal/growth/volume?uid=U1", "", http.StatusUnauthorized},
		{"/internal/growth/volume?uid=U1", "wrong", http.StatusUnauthorized},
		{"/internal/growth/volume", "tok", http.StatusBadRequest},
		{"/internal/growth/volume?uid=U1&days=0", "tok", http.StatusBadRequest},
		{"/internal/growth/volume?uid=U1&days=999", "tok", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		if tc.token != "" {
			req.Header.Set("X-Internal-Token", tc.token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s token=%q: got %d want %d (%s)", tc.url, tc.token, rec.Code, tc.want, rec.Body.String())
		}
	}
}
