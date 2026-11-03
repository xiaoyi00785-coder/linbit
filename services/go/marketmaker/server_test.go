package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The virtual seed must be refused before anything else in production or on mainnet.
func TestSeedRefusedInProductionOrMainnet(t *testing.T) {
	m := &Maker{cfg: Config{UserID: "mm:main"}}
	for _, env := range []map[string]string{
		{"LINBIT_ENV": "production", "WALLET_CHAIN": "mock"},
		{"LINBIT_ENV": "development", "WALLET_CHAIN": "mainnet"},
	} {
		for k, v := range env {
			t.Setenv(k, v)
		}
		w := httptest.NewRecorder()
		m.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/internal/mm/seed", strings.NewReader(`{"ref":"x"}`)))
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "SEED_DISABLED") {
			t.Fatalf("%v: got %d %s", env, w.Code, w.Body.String())
		}
	}
}
