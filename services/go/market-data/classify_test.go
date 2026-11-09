package main

import "net/url"
import "testing"

func TestAllowInDefaultList(t *testing.T) {
	def := url.Values{}
	if !allowInDefaultList("crypto", def) || allowInDefaultList("stock", def) || allowInDefaultList("metal", def) {
		t.Fatal("default list must be crypto only")
	}
	inc := url.Values{"include": []string{"tradfi"}}
	if !allowInDefaultList("stock", inc) || !allowInDefaultList("crypto", inc) {
		t.Fatal("include=tradfi should keep crypto and stocks")
	}
	metal := url.Values{"kind": []string{"metal"}}
	if allowInDefaultList("crypto", metal) || !allowInDefaultList("metal", metal) {
		t.Fatal("kind=metal")
	}
	if classOf(map[string]instClass{"AAPLUSDT": {Market: "spot", Kind: "stock"}}, "BTCUSDT", "spot").Kind != "crypto" {
		t.Fatal("missing symbol is crypto")
	}
	if categoryKind("stocks") != "stock" || categoryKind("metals") != "metal" || categoryKind("layer1") != "crypto" {
		t.Fatal("category kind")
	}
}

func TestFuturesAllowSameRule(t *testing.T) {
	// The futures helper lives in another module; this locks the spot rule
	// that crypto stays and TradFi needs include=tradfi or kind=.
	if allowInDefaultList("commodity", url.Values{}) {
		t.Fatal("commodity leaked into the default list")
	}
}
