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
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/strategy/grid/7/symbol":
			w.Write([]byte(`{"symbol":"PAXGUSDT"}`))
		case "/internal/strategy/grid/8/symbol":
			w.Write([]byte(`{"symbol":"BTCUSDT"}`))
		case "/internal/strategy/grid/9/symbol":
			w.WriteHeader(http.StatusInternalServerError)
		case "/internal/convert/quotes/q-aapl":
			w.Write([]byte(`{"from":"USDT","to":"AAPL"}`))
		case "/internal/convert/quotes/q-paxg":
			w.Write([]byte(`{"from":"PAXG","to":"USDT"}`))
		default:
			if strings.HasPrefix(r.URL.Path, "/internal/") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(`{"ok":true}`))
		}
	}))
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
		// grid
		{"grid create AAPL flag off", "POST", "/strategy/grid", `{"symbol":"AAPLUSDT"}`, "u1", "SG", 403, "FEATURE_DISABLED"},
		{"grid create PAXG US", "POST", "/strategy/grid", `{"symbol":"PAXGUSDT"}`, "u1", "US", 403, "REGION_RESTRICTED"},
		{"grid create BTC US", "POST", "/strategy/grid", `{"symbol":"BTCUSDT"}`, "u1", "US", 200, ""},
		{"grid stop PAXG US", "POST", "/strategy/grid/7/stop", `{}`, "u-us", "US", 200, ""},
		{"grid copy PAXG kyc US", "POST", "/strategy/grid/square/7/copy", `{"investment":"10"}`, "u-us", "", 403, "REGION_RESTRICTED"},
		{"grid copy PAXG SG", "POST", "/strategy/grid/square/7/copy", `{"investment":"10"}`, "u1", "SG", 200, ""},
		{"grid copy BTC US", "POST", "/strategy/grid/square/8/copy", `{"investment":"10"}`, "u1", "US", 200, ""},
		{"grid copy unknown", "POST", "/strategy/grid/square/404/copy", `{}`, "u1", "US", 200, ""},
		{"grid copy lookup down", "POST", "/strategy/grid/square/9/copy", `{}`, "u1", "SG", 503, "SERVICE_UNAVAILABLE"},
		// convert
		{"convert quote to AAPL", "POST", "/convert/quote", `{"from":"USDT","to":"AAPL","fromAmount":"10"}`, "u1", "SG", 403, "FEATURE_DISABLED"},
		{"convert quote from PAXG US", "POST", "/convert/quote", `{"from":"PAXG","to":"USDT","fromAmount":"1"}`, "u1", "US", 403, "REGION_RESTRICTED"},
		{"convert quote BTC US", "POST", "/convert/quote", `{"from":"BTC","to":"USDT","fromAmount":"1"}`, "u1", "US", 404, "NOT_FOUND"}, // passes the gate; /convert not routed on this branch,
		{"convert accept AAPL", "POST", "/convert/accept", `{"quoteId":"q-aapl"}`, "u1", "SG", 403, "FEATURE_DISABLED"},
		{"convert accept PAXG kyc US", "POST", "/convert/accept", `{"quoteId":"q-paxg"}`, "u-us", "", 403, "REGION_RESTRICTED"},
		{"convert accept unknown", "POST", "/convert/accept", `{"quoteId":"q-x"}`, "u1", "US", 404, "NOT_FOUND"},
		// transfers
		{"transfer AAPL flag off", "POST", "/assets/transfer", `{"asset":"AAPL","amount":"1","from":"spot","to":"futures"}`, "u1", "SG", 403, "FEATURE_DISABLED"},
		{"transfer PAXG US", "POST", "/assets/transfer", `{"asset":"paxg","amount":"1"}`, "u1", "US", 403, "REGION_RESTRICTED"},
		{"transfer USDT US", "POST", "/assets/transfer", `{"asset":"USDT","amount":"1"}`, "u1", "US", 200, ""},
		{"sub transfer PAXG kyc US", "POST", "/sub-accounts/transfer", `{"asset":"PAXG","amount":"1"}`, "u-us", "", 403, "REGION_RESTRICTED"},
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
	for _, c := range []struct {
		path, body string
		want       int
	}{
		{"/spot/orders", `{"symbol":"AAPLUSDT"}`, 503},
		{"/spot/orders", `{"symbol":"BTCUSDT"}`, 200},
		{"/strategy/grid", `{"symbol":"PAXGUSDT"}`, 503},
		{"/strategy/grid", `{"symbol":"ETHUSDT"}`, 200},
		{"/assets/transfer", `{"asset":"AAPL","amount":"1"}`, 503},
		{"/assets/transfer", `{"asset":"USDT","amount":"1"}`, 200},
		{"/convert/quote", `{"from":"USDT","to":"XAUT"}`, 503},
	} {
		req := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		req.Header.Set("Authorization", "Bearer "+tk)
		rec := httptest.NewRecorder()
		gw.Handler().ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d", c.path, c.body, rec.Code, c.want)
		}
	}
}
