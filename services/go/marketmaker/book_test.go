package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func sampleBook() ExtBook {
	return ExtBook{
		Source: "binance",
		Bids: []BookLevel{
			{Price: d("100000"), Qty: d("2")},
			{Price: d("99900"), Qty: d("1")},
			{Price: d("99800"), Qty: d("0.5")},
		},
		Asks: []BookLevel{
			{Price: d("100100"), Qty: d("2")},
			{Price: d("100200"), Qty: d("1")},
			{Price: d("100300"), Qty: d("0.5")},
		},
	}
}

func TestBuildFromBookCopiesPrices(t *testing.T) {
	inv := Inventory{AllowBuy: true, AllowSell: true}
	p := defPair()
	p.Levels = 3
	p.MaxSkewBps = d("0")
	mid, qs := BuildFromBook(p, btc, sampleBook(), inv, decimal.Zero, d("1"))
	if !mid.Equal(d("100050")) {
		t.Fatalf("mid %s", mid)
	}
	if len(qs) != 6 {
		t.Fatalf("n=%d", len(qs))
	}
	if qs[0].Side != "buy" || !qs[0].Price.Equal(d("100000")) {
		t.Fatalf("best bid %+v", qs[0])
	}
	if qs[3].Side != "sell" || !qs[3].Price.Equal(d("100100")) {
		t.Fatalf("best ask %+v", qs[3])
	}
	// 2 BTC * 100000 = 200k USDT, under cap
	if !qs[0].Qty.Equal(d("2")) {
		t.Fatalf("qty %s", qs[0].Qty)
	}
}

func TestBuildFromBookClampsNotional(t *testing.T) {
	inv := Inventory{AllowBuy: true, AllowSell: true}
	p := defPair()
	p.Levels = 1
	p.MaxSkewBps = d("0")
	book := ExtBook{
		Source: "binance",
		Bids:   []BookLevel{{Price: d("100000"), Qty: d("100")}}, // 10M USDT -> cap 1M
		Asks:   []BookLevel{{Price: d("100100"), Qty: d("100")}},
	}
	_, qs := BuildFromBook(p, btc, book, inv, decimal.Zero, d("1"))
	if len(qs) != 2 {
		t.Fatalf("n=%d", len(qs))
	}
	if !qs[0].Qty.Mul(qs[0].Price).Equal(d("1000000")) {
		t.Fatalf("bid notional %s", qs[0].Qty.Mul(qs[0].Price))
	}
}

func TestBuildFromBookDropsSide(t *testing.T) {
	p := defPair()
	p.Levels = 2
	p.MaxSkewBps = d("0")
	inv := Inventory{AllowBuy: false, AllowSell: true}
	_, qs := BuildFromBook(p, btc, sampleBook(), inv, decimal.Zero, d("1"))
	for _, q := range qs {
		if q.Side != "sell" {
			t.Fatalf("unexpected %+v", q)
		}
	}
}

func TestBuildFromBookWidenAndSkew(t *testing.T) {
	p := defPair()
	p.Levels = 1
	p.MaxSkewBps = d("20")
	inv := Inventory{AllowBuy: true, AllowSell: true, Imbalance: d("1")} // shift down 20bps
	_, qs := BuildFromBook(p, btc, sampleBook(), inv, d("10"), d("1"))
	var bid, ask Quote
	for _, q := range qs {
		if q.Side == "buy" {
			bid = q
		} else {
			ask = q
		}
	}
	// bid: 100000 * (1-0.0020-0.0010) = 99700
	if !bid.Price.Equal(d("99700")) {
		t.Fatalf("bid %s", bid.Price)
	}
	// ask: 100100 * (1-0.0020+0.0010) = 100100 * 0.999 = 99999.9 -> ceil tick 99999.90? tick 0.01
	// 100100 * 0.999 = 99999.9 -> ceilTo 99999.90
	if !ask.Price.Equal(d("99999.90")) {
		t.Fatalf("ask %s", ask.Price)
	}
	if !bid.Price.LessThan(ask.Price) {
		t.Fatalf("crossed %s/%s", bid.Price, ask.Price)
	}
}

func TestParseBinanceAndOKX(t *testing.T) {
	bn, err := parseSides([][]string{{"95000.0", "1.5"}, {"94990", "2"}}, [][]string{{"95010", "1.2"}}, "binance")
	if err != nil || !bn.Valid() || !bn.Bids[0].Price.Equal(d("95000.0")) {
		t.Fatalf("%+v %v", bn, err)
	}
	ok, err := parseSides([][]string{{"1", "10", "0", "1"}}, [][]string{{"1.1", "8", "0", "1"}}, "okx")
	if err != nil || !ok.Valid() {
		t.Fatalf("%+v %v", ok, err)
	}
}

func TestBookFeedFailoverAndCache(t *testing.T) {
	var bnHits, oxHits int
	bn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bnHits++
		if strings.Contains(r.URL.Path, "/api/v3/depth") {
			http.Error(w, "nope", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bids": [][]string{{"2000", "1"}}, "asks": [][]string{{"2001", "1"}},
		})
	}))
	defer bn.Close()
	ox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oxHits++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "0",
			"data": []map[string]any{{
				"bids": [][]string{{"1999", "3"}},
				"asks": [][]string{{"2002", "2"}},
			}},
		})
	}))
	defer ox.Close()
	f := newBookFeed(Config{
		BookFeed: "binance,okx", BookScale: d("1"), BookTTL: time.Minute,
		BinanceURL: bn.URL, BinanceFAPIURL: bn.URL, OKXURL: ox.URL,
	})
	f.client = http.DefaultClient
	book, err := f.get(context.Background(), "spot", "ETHUSDT", 5)
	if err != nil || book.Source != "okx" || !book.Bids[0].Qty.Equal(d("3")) {
		t.Fatalf("%+v %v", book, err)
	}
	_, _ = f.get(context.Background(), "spot", "ETHUSDT", 5)
	if oxHits != 1 {
		t.Fatalf("cache missed oxHits=%d bnHits=%d", oxHits, bnHits)
	}
}

func TestBookResolveTradFiAndFutures(t *testing.T) {
	f := newBookFeed(Config{BookFeed: "binance,okx", BookScale: d("1")})
	fapi, bn, ox := f.resolve("spot", "PAXGUSDT")
	if !fapi || bn != "XAUUSDT" || ox != "XAU-USDT-SWAP" {
		t.Fatalf("paxg %v %s %s", fapi, bn, ox)
	}
	fapi, bn, ox = f.resolve("futures", "WTIUSDT")
	if !fapi || bn != "CLUSDT" || ox != "CL-USDT-SWAP" {
		t.Fatalf("wti %v %s %s", fapi, bn, ox)
	}
	fapi, bn, ox = f.resolve("futures", "BTCUSDT")
	if !fapi || bn != "BTCUSDT" || ox != "BTC-USDT-SWAP" {
		t.Fatalf("btc fut %v %s %s", fapi, bn, ox)
	}
	fapi, bn, ox = f.resolve("spot", "BTCUSDT")
	if fapi || bn != "BTCUSDT" || ox != "BTC-USDT" {
		t.Fatalf("btc spot %v %s %s", fapi, bn, ox)
	}
}

func TestDepthLimit(t *testing.T) {
	if depthLimit(20) != 20 || depthLimit(7) != 10 || depthLimit(50) != 50 {
		t.Fatal("limit")
	}
}

func TestFuturesNotionalCap(t *testing.T) {
	p := defPair()
	p.Market = "futures"
	p.Leverage = 5
	p.Levels = 20
	cap := quoteNotionalCap(p)
	if !cap.Equal(d("30000")) {
		t.Fatalf("cap %s", cap)
	}
	p.Market = "spot"
	if !quoteNotionalCap(p).Equal(d("1000000")) {
		t.Fatalf("spot cap %s", quoteNotionalCap(p))
	}
}

func TestBookResolveTONFutures(t *testing.T) {
	f := newBookFeed(Config{BookFeed: "binance,okx,bybit", BookScale: d("1")})
	fapi, bn, ox := f.resolve("futures", "TONUSDT")
	if !fapi || bn != "TONUSDT" || ox != "TON-USDT-SWAP" {
		t.Fatalf("ton %v %s %s", fapi, bn, ox)
	}
}

func TestBybitOrderbook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"retCode": 0,
			"result": map[string]any{
				"b": [][]string{{"1.05", "20"}},
				"a": [][]string{{"1.06", "18"}},
			},
		})
	}))
	defer srv.Close()
	f := newBookFeed(Config{BookFeed: "bybit", BookScale: d("1"), BookTTL: time.Minute, BybitURL: srv.URL})
	f.client = http.DefaultClient
	book, err := f.get(context.Background(), "futures", "TONUSDT", 5)
	if err != nil || book.Source != "bybit-linear" || !book.Bids[0].Price.Equal(d("1.05")) {
		t.Fatalf("%+v %v", book, err)
	}
}
