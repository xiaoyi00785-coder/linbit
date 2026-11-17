package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPositionSideFromBodyAndQuery(t *testing.T) {
	body := httptest.NewRequest(http.MethodPost, "/futures/positions/BTCUSDT/close?positionSide=SHORT", strings.NewReader(`{"positionSide":"LONG"}`))
	if got := positionSideFrom(body); got != "LONG" {
		t.Fatalf("body wins, got %q", got)
	}
	query := httptest.NewRequest(http.MethodPost, "/futures/positions/BTCUSDT/close?positionSide=SHORT", nil)
	if got := positionSideFrom(query); got != "SHORT" {
		t.Fatalf("query, got %q", got)
	}
	empty := httptest.NewRequest(http.MethodPost, "/futures/positions/BTCUSDT/close", strings.NewReader(""))
	if got := positionSideFrom(empty); got != "" {
		t.Fatalf("empty, got %q", got)
	}
}
