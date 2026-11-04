package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNetworkDisplayDefaults(t *testing.T) {
	r := newRegistry(defaultNetworks())
	got := make([]networkDTO, 0)
	for _, n := range r.ForAsset("USDT") {
		got = append(got, toNetworkDTO(n))
	}
	sortNetworks(got)
	order := []string{}
	for _, n := range got {
		order = append(order, n.Network)
		if n.Recommended != (n.Network == "TRC20") {
			t.Fatalf("%s recommended=%v", n.Network, n.Recommended)
		}
	}
	if want := "TRC20,ERC20,BEP20,ARBITRUM"; join(order) != want {
		t.Fatalf("order=%s want %s", join(order), want)
	}
	if got[0].SortOrder != 10 || got[3].SortOrder != 40 {
		t.Fatalf("sortOrder=%d..%d", got[0].SortOrder, got[3].SortOrder)
	}
	for _, n := range r.ForAsset("USDC") {
		if n.Recommended {
			t.Fatalf("USDC %s recommended by default", n.Network)
		}
	}
}

func TestNetworkDisplayFromEnvJSON(t *testing.T) {
	t.Setenv("WALLET_NETWORKS_JSON", `[
		{"asset":"USDT","network":"ERC20","minWithdraw":"12"},
		{"asset":"USDT","network":"TRC20","minWithdraw":"12"},
		{"asset":"USDT","network":"BEP20","minWithdraw":"12","recommended":true,"sortOrder":1}]`)
	r := loadNetworks()
	m := map[string]Network{}
	for _, n := range r.ForAsset("USDT") {
		m[n.Network] = n
	}
	if m["ERC20"].SortOrder != 10 || m["TRC20"].SortOrder != 20 || m["BEP20"].SortOrder != 1 {
		t.Fatalf("sortOrder %d %d %d", m["ERC20"].SortOrder, m["TRC20"].SortOrder, m["BEP20"].SortOrder)
	}
	if !m["TRC20"].Recommended || !m["BEP20"].Recommended || m["ERC20"].Recommended {
		t.Fatalf("recommended %v %v %v", m["TRC20"].Recommended, m["BEP20"].Recommended, m["ERC20"].Recommended)
	}
}

func TestNetworkDisplayOverlayAndSort(t *testing.T) {
	t.Cleanup(func() { setOverlay(map[string]netOverride{}) })
	f, five := false, 5
	setOverlay(map[string]netOverride{pairKey("USDT", "TRC20"): {Recommended: &f, SortOrder: &five}})
	n := applyOverlay(Network{Asset: "USDT", Network: "TRC20", Recommended: true, SortOrder: 10})
	if n.Recommended || n.SortOrder != 5 {
		t.Fatalf("overlay not applied: %+v", n)
	}
	l := []networkDTO{{Asset: "USDT", Network: "B", SortOrder: 5}, {Asset: "USDT", Network: "A", SortOrder: 5}, {Asset: "BTC", Network: "BTC", SortOrder: 99}, {Asset: "USDT", Network: "Z", SortOrder: 1}}
	sortNetworks(l)
	if l[0].Asset != "BTC" || l[1].Network != "Z" || l[2].Network != "A" || l[3].Network != "B" {
		t.Fatalf("sort: %+v", l)
	}
	b, _ := json.Marshal(toNetworkDTO(n))
	if !bytes.Contains(b, []byte(`"recommended":false`)) || !bytes.Contains(b, []byte(`"sortOrder":5`)) {
		t.Fatalf("json: %s", b)
	}
}

// TestAdminNetworkDisplayRoundTrip: PUT recommended/sortOrder persists, shows in
// GET, partial update keeps the other field, reset restores defaults.
func TestAdminNetworkDisplayRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	t.Cleanup(func() { setOverlay(map[string]netOverride{}) })
	cfg := testServerConfig(t, "", "")
	cfg.Networks = newRegistry(defaultNetworks())
	s := NewServer(cfg, store)
	put := func(body string) map[string]networkDTO {
		req := httptest.NewRequest(http.MethodPut, "/internal/admin/wallet/networks/USDT/ERC20", bytes.NewBufferString(body))
		req.SetPathValue("asset", "USDT")
		req.SetPathValue("network", "ERC20")
		w := httptest.NewRecorder()
		s.handleAdminPutNetwork(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s -> %d %s", body, w.Code, w.Body)
		}
		var out map[string]networkDTO
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	_ = put(`{"reset":true}`)
	if a := put(`{"recommended":true,"sortOrder":1,"updatedBy":"t"}`)["after"]; !a.Recommended || a.SortOrder != 1 {
		t.Fatalf("after=%+v", a)
	}
	if a := put(`{"sortOrder":3}`)["after"]; !a.Recommended || a.SortOrder != 3 {
		t.Fatalf("partial after=%+v", a)
	}
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewBufferString(`{"sortOrder":1000000}`))
	req.SetPathValue("asset", "USDT")
	req.SetPathValue("network", "ERC20")
	w := httptest.NewRecorder()
	s.handleAdminPutNetwork(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range sortOrder -> %d", w.Code)
	}
	if a := put(`{"reset":true}`)["after"]; a.Recommended || a.SortOrder != 20 {
		t.Fatalf("reset after=%+v", a)
	}
}

func join(s []string) string {
	o := ""
	for i, x := range s {
		if i > 0 {
			o += ","
		}
		o += x
	}
	return o
}
