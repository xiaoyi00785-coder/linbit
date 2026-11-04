package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewProxyUpstreamDownReturnsJSON502(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(ts.URL)
	ts.Close() // upstream unreachable
	rec := httptest.NewRecorder()
	newProxy(u).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/futures/positions", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Fatalf("content-type=%q", rec.Header().Get("Content-Type"))
	}
	var b map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if !strings.Contains(rec.Body.String(), "UPSTREAM_UNAVAILABLE") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
