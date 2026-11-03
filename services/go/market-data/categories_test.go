package main

import "testing"

func TestFilterCategories(t *testing.T) {
	cats := []Category{{ID: "layer1", Name: "公链", Symbols: []string{"BTCUSDT", "SOLUSDT", "XRPUSDT"}}, {ID: "metals", Name: "贵金属", Symbols: []string{}}}
	got := filterCategories(cats, map[string]bool{"BTCUSDT": true, "SOLUSDT": true})
	if len(got) != 2 || len(got[0].Symbols) != 2 || got[0].Symbols[1] != "SOLUSDT" {
		t.Fatalf("layer1 = %+v", got)
	}
	if got[1].Symbols == nil || len(got[1].Symbols) != 0 {
		t.Fatalf("metals must be empty list, got %#v", got[1].Symbols)
	}
}
