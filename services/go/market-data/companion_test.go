package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPorCannotSelfApprove(t *testing.T) {
	if err := porDecision("approve", "alice", "alice", "alice", "pending_review"); err == nil {
		t.Fatal("author approved")
	}
	if err := porDecision("approve", "alice", "bob", "alice", "pending_review"); err == nil {
		t.Fatal("submitter approved")
	}
	if err := porDecision("approve", "cara", "alice", "bob", "pending_review"); err != nil {
		t.Fatal(err)
	}
	if err := porDecision("approve", "cara", "", "bob", "pending_review"); err == nil {
		t.Fatal("empty author")
	}
	if err := porDecision("submit", "alice", "alice", "", "draft"); err != nil {
		t.Fatal(err)
	}
}

func TestAcademyAndSearchEmpty(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleAcademyCategories(w, httptest.NewRequest(http.MethodGet, "/academy/categories", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("categories %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleMarketSearch(w, httptest.NewRequest(http.MethodGet, "/market/search?q=btc", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"articles":[]`) {
		t.Fatalf("search %d %s", w.Code, w.Body.String())
	}
}

func TestUSDRateUnavailable(t *testing.T) {
	resetUSDCache()
	s := &Server{cfg: Config{IndexUsdtUsdMin: "0.97", IndexUsdtUsdMax: "1.03"}}
	w := httptest.NewRecorder()
	s.handleUSDRate(w, httptest.NewRequest(http.MethodGet, "/market/usd-rate", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), `"pegAssumed":false`) {
		t.Fatalf("%s", w.Body.String())
	}
	_ = time.Now()
}
