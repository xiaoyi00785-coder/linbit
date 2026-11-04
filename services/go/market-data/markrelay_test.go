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

func TestFuturesRelayChannels(t *testing.T) {
	for ch, ok := range map[string]bool{
		"mark:BTCUSDT": true, "futures-ticker:BTCUSDT": true, "futures-kline:BTCUSDT:1m": true, "futures-kline:BTCUSDT:1w": true,
		"futures-kline:BTCUSDT:7m": false, "futures-kline:BTCUSDT": false, "futures-ticker:btcusdt": false,
		"index:BTCUSDT": false, "ticker:BTCUSDT": false, "kline:BTCUSDT:1m": false,
	} {
		if relayChannelRe.MatchString(ch) != ok {
			t.Errorf("relay %s: want %v", ch, ok)
		}
	}
	c := newTestClient(NewHub(fakeSnap{}))
	for _, ch := range []string{"index:BTCUSDT", "futures-ticker:BTCUSDT", "futures-kline:BTCUSDT:4h"} {
		if code, _ := c.validateChannel(ch); code != "" {
			t.Errorf("%s rejected: %s", ch, code)
		}
	}
	for _, ch := range []string{"index:", "index:BTC:1", "futures-ticker:", "futures-kline:BTCUSDT:7m", "futures-kline:BTCUSDT"} {
		if code, _ := c.validateChannel(ch); code == "" {
			t.Errorf("%s accepted", ch)
		}
	}
}

func TestIndexAliasOfMark(t *testing.T) {
	h := NewHub(fakeSnap{})
	c := newTestClient(h)
	h.subscribe(c, "index:BTCUSDT")
	c.subs["index:BTCUSDT"] = struct{}{}
	h.Relay("mark:BTCUSDT", json.RawMessage(`{"indexPrice":"7"}`))
	var f struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(nextFrame(t, c), &f); err != nil || f.Channel != "index:BTCUSDT" || string(f.Data) != `{"indexPrice":"7"}` {
		t.Fatalf("frame %+v %v", f, err)
	}
	// late subscriber gets the remembered snapshot under the alias name
	c2 := newTestClient(h)
	c2.doSubscribe("1", "index:BTCUSDT")
	nextFrame(t, c2) // receipt
	if err := json.Unmarshal(nextFrame(t, c2), &f); err != nil || f.Channel != "index:BTCUSDT" {
		t.Fatalf("snapshot %+v %v", f, err)
	}
}
