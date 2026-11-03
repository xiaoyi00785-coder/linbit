package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)


func pubServer(t *testing.T, marketURL string) *Server {
	c := Contract{Symbol: "BTCUSDT", Base: "BTC", Quote: "USDT", Margin: "USDT", MaxLeverage: 100, MMR: d("0.005"), Tick: d("0.1"), MinQty: d("0.001"), MaxQty: d("100"), FundingH: 8,
		Tiers: []Tier{{Tier: 1, MaxNotional: d("50000"), MMR: d("0.004"), MaxLeverage: 125}, {Tier: 2, MaxNotional: d("250000"), MMR: d("0.005"), MaintAmount: d("50"), MaxLeverage: 100}}}
	s := &Server{cfg: Config{DefaultLeverage: 20, DefaultMarginMode: "cross", LiqFeeRate: decimal.NewFromInt(-1), MarkSampleEvery: time.Minute},
		contracts: map[string]Contract{"BTCUSDT": c}, symbols: []string{"BTCUSDT"}, prices: map[string]*priceState{"BTCUSDT": {Index: d("60000"), Mark: d("60010"), IndexAt: time.Now()}},
		market: NewMarketClient(marketURL)}
	return s
}

func get(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	return rr.Code, m
}

func TestRiskTiersPublic(t *testing.T) {
	h := pubServer(t, "").Routes()
	code, m := get(t, h, "/futures/market/risk-tiers?symbol=btcusdt")
	if code != 200 {
		t.Fatalf("code %d %v", code, m)
	}
	tiers := m["contracts"].([]any)[0].(map[string]any)["tiers"].([]any)
	t2 := tiers[1].(map[string]any)
	if len(tiers) != 2 || t2["minNotional"] != "50000" || t2["maxNotional"] != "250000" || t2["maxLeverage"].(float64) != 100 {
		t.Fatalf("tiers %v", tiers)
	}
	if code, _ := get(t, h, "/futures/market/risk-tiers?symbol=NOPE"); code != 400 {
		t.Fatalf("unknown symbol code %d", code)
	}
}

func TestIsolatedParamsPublic(t *testing.T) {
	code, m := get(t, pubServer(t, "").Routes(), "/futures/market/isolated-params")
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	c := m["contracts"].([]any)[0].(map[string]any)
	if c["liquidationFeeRate"] != "0.005" || c["maintenanceMarginRate"] != "0.004" || c["defaultLeverage"].(float64) != 20 {
		t.Fatalf("params %v", c)
	}
}

func TestIndexPublicComponents(t *testing.T) {
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") != "BTCUSDT" {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(`{"symbol":"BTCUSDT","indexPrice":"60000","source":"binance","components":[{"exchange":"binance","price":"60000","weight":"1","fresh":true},{"exchange":"okx","price":"60001","weight":"0","fresh":true}]}`))
	}))
	defer md.Close()
	code, m := get(t, pubServer(t, md.URL).Routes(), "/futures/market/index?symbol=BTCUSDT")
	if code != 200 || m["source"] != "binance" || m["markPrice"] != "60010" {
		t.Fatalf("code %d %v", code, m)
	}
	if cs := m["components"].([]any); len(cs) != 2 || cs[0].(map[string]any)["weight"] != "1" {
		t.Fatalf("components %v", cs)
	}
}

func TestTimeRangeAndInterval(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?startTime=2000&endTime=1000", nil)
	if _, _, err := timeRange(r); err == nil {
		t.Fatal("want error for end<start")
	}
	r = httptest.NewRequest(http.MethodGet, "/x?startTime=1000", nil)
	st, en, err := timeRange(r)
	if err != nil || st == nil || en != nil || st.UnixMilli() != 1000 {
		t.Fatalf("%v %v %v", st, en, err)
	}
	code, _ := get(t, pubServer(t, "").Routes(), "/futures/market/mark-history?symbol=BTCUSDT&interval=7m")
	if code != 400 {
		t.Fatalf("bad interval code %d", code)
	}
}
