package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestOKXInstNotFoundIsNotListed(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"code":"51001","data":[],"msg":"Instrument ID doesn't exist."}`))
	}))
	defer ts.Close()
	now := time.Now()
	_, err := withRetry(context.Background(), 3, time.Millisecond, func() ([]fundingPoint, error) {
		return fetchOKXFunding(context.Background(), ts.Client(), ts.URL, "TON-USDT-SWAP", now.Add(-time.Hour), now)
	})
	if !errors.Is(err, errNotListed) {
		t.Fatalf("want errNotListed, got %v", err)
	}
	if hits != 1 {
		t.Fatalf("not-listed must not be retried, hits=%d", hits)
	}
}

func TestBinanceInvalidSymbolIsNotListed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":-1121,"msg":"Invalid symbol."}`))
	}))
	defer ts.Close()
	now := time.Now()
	_, err := fetchBinanceFunding(context.Background(), ts.Client(), ts.URL, "WTIUSDT", now.Add(-time.Hour), now)
	if !errors.Is(err, errNotListed) {
		t.Fatalf("want errNotListed, got %v", err)
	}
}

func TestTransientRetriedWithBackoff(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"code":"0","data":[]}`))
	}))
	defer ts.Close()
	now := time.Now()
	_, err := withRetry(context.Background(), 3, time.Millisecond, func() ([]fundingPoint, error) {
		return fetchOKXFunding(context.Background(), ts.Client(), ts.URL, "CL-USDT-SWAP", now.Add(-time.Hour), now)
	})
	if err != nil || hits != 3 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

func TestOKXInstID(t *testing.T) {
	if okxInstID("TONUSDT") != "TON-USDT-SWAP" || okxInstID("FOO") != "" {
		t.Fatal("okxInstID")
	}
}
