package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFiatRates(t *testing.T) {
	if r, _, ok := parseFiat([]byte(`{"base":"USD","date":"2026-11-02","rates":{"CNY":7.1,"EUR":0.9,"JPY":151}}`)); !ok || r["USD"] != 1 || r["CNY"] != 7.1 {
		t.Fatal("frankfurter parse")
	}
	if _, _, ok := parseFiat([]byte(`{"result":"error","rates":{"CNY":7,"EUR":1,"JPY":2}}`)); ok {
		t.Fatal("error result accepted")
	}
	fiatCache.good = nil
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer down.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"result":"success","time_last_update_unix":1762128000,"rates":{"USD":1,"CNY":7.2,"EUR":0.93,"HKD":7.77}}`))
	}))
	s := &Server{cfg: Config{FiatRatesURLs: []string{down.URL}}}
	get := func(q string) (int, map[string]any) {
		w := httptest.NewRecorder()
		s.handleFiatRates(w, httptest.NewRequest("GET", "/market/fiat-rates"+q, nil))
		var m map[string]any
		json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}
	if c, m := get(""); c != 200 || m["source"] != "static" || m["base"] != "USDT" {
		t.Fatalf("static fallback: %d %v", c, m)
	}
	if c, _ := get("?base=BTC"); c != 400 {
		t.Fatal("bad base accepted")
	}
	s.cfg.FiatRatesURLs = []string{down.URL, up.URL}
	fiatCache.checked = fiatCache.checked.Add(-2 * fiatTTL)
	c, m := get("?base=usdt")
	if c != 200 || m["source"] == "static" || m["rates"].(map[string]any)["CNY"] != 7.2 || m["stale"] != false {
		t.Fatalf("fallback source: %d %v", c, m)
	}
	up.Close()
	fiatCache.checked = fiatCache.checked.Add(-2 * fiatTTL)
	if _, m := get(""); m["stale"] != true || m["rates"].(map[string]any)["CNY"] != 7.2 {
		t.Fatalf("last good not kept: %v", m)
	}
	fiatCache.good = nil
}
