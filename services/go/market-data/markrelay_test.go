package main

import (
	"encoding/json"
	"testing"
)

func TestMarkChannelRelay(t *testing.T) {
	for ch, ok := range map[string]bool{"mark:BTCUSDT": true, "mark:btcusdt": false, "mark:": false, "ticker:BTCUSDT": false, "mark:BTC:1": false} {
		if markChannelRe.MatchString(ch) != ok {
			t.Errorf("%s: want %v", ch, ok)
		}
	}
	h := NewHub(nil)
	h.Relay("mark:BTCUSDT", json.RawMessage(`{"markPrice":"1"}`))
	if string(h.last["mark:BTCUSDT"]) != `{"markPrice":"1"}` {
		t.Fatalf("last=%s", h.last["mark:BTCUSDT"])
	}
}
