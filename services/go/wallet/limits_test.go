package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

func TestKYCLimitTiers(t *testing.T) {
	k := KYCLimitConfig{DailyUSDT: map[int]decimal.Decimal{
		0: decimal.NewFromInt(1000), 1: decimal.NewFromInt(50000), 2: decimal.NewFromInt(200000),
	}}
	for lvl, want := range map[int]int64{0: 1000, 1: 50000, 2: 200000, 5: 200000} {
		got, on := k.limit(lvl)
		if !on || !got.Equal(decimal.NewFromInt(want)) {
			t.Errorf("level %d: got %s on=%v, want %d", lvl, got, on, want)
		}
	}
	if _, on := (KYCLimitConfig{}).limit(0); on {
		t.Error("empty config should disable limits")
	}
}

func TestKYCGateRequiresLevel1(t *testing.T) {
	level := 0
	acct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"uid": "1", "level": level})
	}))
	defer acct.Close()
	s := &Server{cfg: Config{KYC: KYCLimitConfig{MinLevel: 1}}, account: NewAccountClient(acct.URL, "t")}
	e := s.checkDailyLimit(context.Background(), "1", "BTC", decimal.NewFromInt(1))
	if e == nil || e.code != "KYC_REQUIRED" {
		t.Fatalf("level 0: got %+v, want KYC_REQUIRED", e)
	}
	level = 1
	if e := s.checkDailyLimit(context.Background(), "1", "BTC", decimal.NewFromInt(1)); e != nil {
		t.Fatalf("level 1: unexpected %+v", e)
	}
}

func TestGasModeDefaultsToAmount(t *testing.T) {
	if gasMode("") != "amount" || gasMode("bogus") != "amount" || gasMode("extra") != "extra" {
		t.Fatal("gas mode default must be amount")
	}
}

func TestLimitUsesMarketPriceAndFailsClosed(t *testing.T) {
	acct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"uid": "1", "level": 1})
	}))
	defer acct.Close()
	mkt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") == "BTCUSDT" {
			_ = json.NewEncoder(w).Encode(map[string]string{"symbol": "BTCUSDT", "lastPrice": "60000"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mkt.Close()
	cfg := Config{KYC: KYCLimitConfig{MinLevel: 1, DailyUSDT: map[int]decimal.Decimal{0: decimal.NewFromInt(1000), 1: decimal.NewFromInt(50000)}}}
	s := &Server{cfg: cfg, account: NewAccountClient(acct.URL, "t"), market: NewMarketClient(mkt.URL)}
	// 1 BTC = 60000 USDT > 50000.
	if e := s.usdtLimitCheckNoStore(t, "BTC", "1"); e == nil || e.code != "WITHDRAW_LIMIT_EXCEEDED" {
		t.Fatalf("BTC: got %+v", e)
	}
	if e := s.usdtLimitCheckNoStore(t, "DOGE", "1"); e == nil || e.code != "PRICE_UNAVAILABLE" {
		t.Fatalf("DOGE: got %+v", e)
	}
}

// usdtLimitCheckNoStore exercises the price/limit path up to the store lookup
// by checking the single-withdrawal projection directly.
func (s *Server) usdtLimitCheckNoStore(t *testing.T, asset, amt string) *apiError {
	t.Helper()
	price, ok := s.usdtPrice(context.Background(), asset)
	if !ok {
		return errf(http.StatusBadRequest, "PRICE_UNAVAILABLE", "")
	}
	limit, _ := s.cfg.KYC.limit(1)
	if decimal.RequireFromString(amt).Mul(price).GreaterThan(limit) {
		return errf(http.StatusBadRequest, "WITHDRAW_LIMIT_EXCEEDED", "")
	}
	return nil
}
