package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBackfillSkipsUnlisted(t *testing.T) {
	var kl atomic.Int32
	var bad atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v3/ticker/price"):
			w.Write([]byte(`[{"symbol":"BTCUSDT","price":"100"}]`))
		case strings.HasPrefix(r.URL.Path, "/api/v3/klines"):
			kl.Add(1)
			if r.URL.Query().Get("symbol") != "BTCUSDT" {
				bad.Add(1)
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	a := NewAggregator(nil, []string{"BTCUSDT", "XAUUSDT", "AAPLUSDT"})
	a.BackfillIndex(context.Background(), srv.URL)
	if bad.Load() != 0 || kl.Load() == 0 {
		t.Fatalf("klines=%d bad=%d", kl.Load(), bad.Load())
	}
}
