package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testWalletServer() *Server {
	return &Server{cfg: Config{WithdrawNetworks: defaultWithdrawNetworks()}}
}

func TestWithdrawNetworks(t *testing.T) {
	s := testWalletServer()
	r := httptest.NewRequest("GET", "/wallet/withdraw/networks?asset=USDT", nil)
	r.Header.Set("X-User-Id", "80000001")
	w := httptest.NewRecorder()
	s.handleWithdrawNetworks(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out withdrawNetworkOptionsDTO
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Asset != "USDT" || len(out.Networks) != 2 {
		t.Fatalf("unexpected options: %+v", out)
	}
	if out.Networks[0].GasFeeEstimate == "" {
		t.Fatalf("missing gasFeeEstimate: %+v", out.Networks[0])
	}
}

// TestWithdrawNetworksUSDTMinWithdraw checks that every default USDT network
// shares the same 12 USDT minimum withdrawal.
func TestWithdrawNetworksUSDTMinWithdraw(t *testing.T) {
	nets := defaultWithdrawNetworks()["USDT"]
	if len(nets) == 0 {
		t.Fatal("no default USDT networks")
	}
	for _, n := range nets {
		if n.MinWithdraw != "12.00000000" {
			t.Fatalf("%s minWithdraw = %q, want 12.00000000", n.Network, n.MinWithdraw)
		}
	}
}

func TestWithdrawNetworksRequiresAuth(t *testing.T) {
	s := testWalletServer()
	r := httptest.NewRequest("GET", "/wallet/withdraw/networks?asset=USDT", nil)
	w := httptest.NewRecorder()
	s.handleWithdrawNetworks(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWithdrawNetworksMissingAsset(t *testing.T) {
	s := testWalletServer()
	r := httptest.NewRequest("GET", "/wallet/withdraw/networks", nil)
	r.Header.Set("X-User-Id", "80000001")
	w := httptest.NewRecorder()
	s.handleWithdrawNetworks(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestWithdrawNetworksUnknownAssetEmpty(t *testing.T) {
	s := testWalletServer()
	r := httptest.NewRequest("GET", "/wallet/withdraw/networks?asset=DOGE", nil)
	r.Header.Set("X-User-Id", "80000001")
	w := httptest.NewRecorder()
	s.handleWithdrawNetworks(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out withdrawNetworkOptionsDTO
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Networks == nil || len(out.Networks) != 0 {
		t.Fatalf("unknown asset networks = %v, want empty (non-null)", out.Networks)
	}
}

func TestWithdrawRecordsEmpty(t *testing.T) {
	s := testWalletServer()
	r := httptest.NewRequest("GET", "/wallet/withdraw/records?page=2&pageSize=10", nil)
	r.Header.Set("X-User-Id", "80000001")
	w := httptest.NewRecorder()
	s.handleWithdrawRecords(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out paginatedWithdrawals
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Items == nil || len(out.Items) != 0 || out.Total != 0 {
		t.Fatalf("records = %+v, want empty list", out)
	}
	if out.Page != 2 || out.PageSize != 10 {
		t.Fatalf("pagination = page %d size %d, want 2/10", out.Page, out.PageSize)
	}
}
