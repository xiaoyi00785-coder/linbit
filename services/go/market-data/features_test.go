package main

import "testing"

func TestMergeFeatures(t *testing.T) {
	f := mergeFeatures(nil)
	if !f["convert"] || !f["supportTickets"] || f["tokenizedStocks"] || f["chat"] || f["subAccounts"] || f["metals"] || f["eventContracts"] {
		t.Fatalf("defaults: %v", f)
	}
	f = mergeFeatures(map[string]bool{"chat": true, "convert": false, "stale": true})
	if !f["chat"] || f["convert"] {
		t.Fatalf("override: %v", f)
	}
	if _, ok := f["stale"]; ok {
		t.Fatal("unknown key leaked")
	}
}
