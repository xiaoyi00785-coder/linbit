package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseExtBinanceKlineAndTrade(t *testing.T) {
	kl := parseExtBinanceKlines([][]json.RawMessage{{
		raw(`1499040000000`), raw(`"0.016"`), raw(`"0.800"`), raw(`"0.015"`), raw(`"0.200"`), raw(`"148976.1"`),
	}})
	if len(kl) != 1 || kl[0].Time != 1499040000 || kl[0].Volume != "148976.1" || kl[0].Close != "0.200" || kl[0].Source != "" {
		t.Fatalf("%+v", kl)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/v3/klines"):
			w.WriteHeader(400)
		case strings.Contains(r.URL.Path, "/fapi/v1/klines"):
			w.Write([]byte(`[[1499040000000,"1","2","0.5","1.5","9"]]`))
		case strings.Contains(r.URL.Path, "/fapi/v1/aggTrades"):
			w.Write([]byte(`[{"a":7,"p":"1.5","q":"3","T":1499040000000,"m":true}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	e := newExtFeed(srv.URL, srv.URL, srv.URL)
	rows, ok := e.Klines(context.Background(), "AAPLUSDT", "1m", 10)
	if !ok || len(rows) != 1 || rows[0].Volume != "9" {
		t.Fatalf("%+v %v", rows, ok)
	}
	tr, ok := e.Trades(context.Background(), "AAPLUSDT", 10)
	if !ok || tr[0].Side != "sell" || tr[0].TradeID != "7" {
		t.Fatalf("%+v %v", tr, ok)
	}
	if !e.live("AAPLUSDT") {
		t.Fatal("expected live")
	}
}

func TestOKXBarAndAlias(t *testing.T) {
	if okxBar("4h") != "4H" || okxBar("1d") != "1D" || okxInst("WTIUSDT", true) != "WTI-USDT-SWAP" {
		t.Fatalf("%s %s", okxBar("4h"), okxInst("WTIUSDT", true))
	}
	if extFAPIAlias["WTIUSDT"] != "CLUSDT" || extFAPIAlias["PAXGUSDT"] != "XAUUSDT" {
		t.Fatal("alias")
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }
