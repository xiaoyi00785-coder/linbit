package main

import "testing"

func TestApplyFeatureDefaultsMainnet(t *testing.T) {
	saved := map[string]bool{}
	for k, v := range featureDefaults {
		saved[k] = v
	}
	defer func() {
		for k, v := range saved {
			featureDefaults[k] = v
		}
	}()
	applyFeatureDefaults(true, "options=false,chat=false")
	for _, k := range []string{"tokenizedStocks", "metals", "eventContracts", "convert"} {
		if !featureDefaults[k] {
			t.Fatalf("%s should be on for mainnet", k)
		}
	}
	if featureDefaults["chat"] || featureDefaults["p2p"] {
		t.Fatal("override/unbuilt flags should be off")
	}
	cs := comingSoon(featureDefaults)
	if len(cs) == 0 || cs[0] != "chat" {
		t.Fatalf("comingSoon = %v", cs)
	}
}
