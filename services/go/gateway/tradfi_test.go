package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linbit/platform/services/go/common"
)

func TestTradFiGuard(t *testing.T) {
	flags := map[string]bool{"tokenizedStocks": false, "metals": true}
	market := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/tradfi/gate" {
			json.NewEncoder(w).Encode(map[string]any{
				"features": flags, "restrictedCountries": []string{"US", "IR"},
				"spotSymbols":    map[string]string{"AAPLUSDT": "tokenizedStocks", "PAXGUSDT": "metals"},
				"futuresSymbols": map[string]string{"XAGUSDT": "metals"},
				"assets":         map[string]string{"AAPL": "tokenizedStocks", "PAXG": "metals"}})
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer market.Close()
	var kycCalls int32
	kyc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&kycCalls, 1)
		c := ""
		if r.URL.Query().Get("userId") == "u-us" {
			c = "us"
		}
		json.NewEncoder(w).Encode(map[string]string{"country": c})
	}))
	defer kyc.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer up.Close()
	cfg := Config{JWTSecret: "test-secret", AccountURL: up.URL, AssetURL: up.URL, WalletURL: up.URL, OrderURL: up.URL,
		MarketURL: market.URL, KycURL: kyc.URL, FuturesURL: up.URL, StrategyURL: up.URL, GrowthURL: up.URL, EarnURL: up.URL}
	gw, err := NewGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := gw.Handler()
	tok := func(uid string) string {
		s, err := common.SignAccessToken("test-secret", uid, uid+"@x.test", time.Hour, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	do := func(method, path, body, uid, cc string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if uid != "" {
			req.Header.Set("Authorization", "Bearer "+tok(uid))
		}
		if cc != "" {
			req.Header.Set("CF-IPCountry", cc)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var e struct {
			Code string `json:"code"`
		}
		json.Unmarshal(rec.Body.Bytes(), &e)
		return rec.Code, e.Code
	}
	cases := []struct {
		name, method, path, body, uid, cc string
		want                              int
		code                              string
	}{
		{"crypto passes", "POST", "/spot/orders", `{"symbol":"BTCUSDT"}`, "u1", "US", 200, ""},
		{"stock flag off", "POST", "/spot/orders", `{"symbol":"AAPLUSDT"}`, "u1", "SG", 403, "FEATURE_DISABLED"},
		{"metal ok", "POST", "/spot/orders", `{"symbol":"PAXGUSDT"}`, "u1", "SG", 200, ""},
		{"metal ip US", "POST", "/spot/orders", `{"symbol":"paxgusdt"}`, "u1", "US", 403, "REGION_RESTRICTED"},
		{"metal kyc US", "POST", "/spot/orders", `{"symbol":"PAXGUSDT"}`, "u-us", "SG", 403, "REGION_RESTRICTED"},
		{"futures XAG kyc US", "POST", "/futures/orders", `{"symbol":"XAGUSDT"}`, "u-us", "", 403, "REGION_RESTRICTED"},
		{"futures BTC kyc US", "POST", "/futures/orders", `{"symbol":"BTCUSDT"}`, "u-us", "", 200, ""},
		{"deposit addr PAXG IR", "GET", "/wallet/deposit/address?asset=PAXG&network=ETH", "", "u1", "IR", 403, "REGION_RESTRICTED"},
		{"withdraw AAPL flag off", "POST", "/wallet/withdraw", `{"asset":"AAPL"}`, "u1", "", 403, "FEATURE_DISABLED"},
		{"catalogue anon ok", "GET", "/tradfi/assets", "", "", "SG", 200, ""},
		{"catalogue anon US", "GET", "/tradfi/assets", "", "", "US", 403, "REGION_RESTRICTED"},
	}
	for _, c := range cases {
		got, code := do(c.method, c.path, c.body, c.uid, c.cc)
		if got != c.want || code != c.code {
			t.Errorf("%s: got %d %q want %d %q", c.name, got, code, c.want, c.code)
		}
	}
	if n := atomic.LoadInt32(&kycCalls); n > 3 {
		t.Errorf("kyc country should be cached, got %d calls", n)
	}
}

func TestTradFiGuardFailClosed(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer up.Close()
	cfg := Config{JWTSecret: "s", AccountURL: up.URL, AssetURL: up.URL, WalletURL: up.URL, OrderURL: up.URL,
		MarketURL: "http://127.0.0.1:1", KycURL: up.URL, FuturesURL: up.URL, StrategyURL: up.URL, GrowthURL: up.URL, EarnURL: up.URL}
	gw, _ := NewGateway(cfg)
	tk, _ := common.SignAccessToken("s", "u1", "u1@x.test", time.Hour, time.Now())
	for sym, want := range map[string]int{"AAPLUSDT": 503, "BTCUSDT": 200} {
		req := httptest.NewRequest("POST", "/spot/orders", strings.NewReader(`{"symbol":"`+sym+`"}`))
		req.Header.Set("Authorization", "Bearer "+tk)
		rec := httptest.NewRecorder()
		gw.Handler().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: got %d want %d", sym, rec.Code, want)
		}
	}
}
