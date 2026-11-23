package main

import "testing"

func TestStripColdDestDropsCustomTargets(t *testing.T) {
	m := map[string]any{
		"asset": "USDT", "network": "TRC20", "amount": "10",
		"destination": "Tattacker", "to": "0xevil", "toAddress": "x",
		"cold": "ycold", "address": "z", "target": "t",
	}
	stripColdDest(m)
	for _, k := range []string{"destination", "to", "toAddress", "cold", "address", "target"} {
		if _, ok := m[k]; ok {
			t.Fatalf("client %s leaked through", k)
		}
	}
	if m["asset"] != "USDT" || m["amount"] != "10" {
		t.Fatalf("stripped too much: %#v", m)
	}
}
