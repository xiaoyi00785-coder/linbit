package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestOKXTradeBookAndCandle(t *testing.T) {
	e := newExtFeed("https://api.binance.com", "https://fapi.binance.com", "https://www.okx.com")
	bc := &recBroadcaster{}
	sym := streamSym{local: "BTCUSDT", binance: "BTCUSDT", fut: true}
	by := map[string][]streamSym{"BTC-USDT-SWAP": {sym}}

	e.onOKX([]byte(`{"arg":{"channel":"trades","instId":"BTC-USDT-SWAP"},"data":[{"tradeId":"9","px":"64510.1","sz":"0.02","side":"sell","ts":"1794820000000"}]}`), by, bc)
	if bc.count("futures-trades:BTCUSDT") != 1 || bc.count("futures-ticker:BTCUSDT") != 1 {
		t.Fatalf("trade frames %+v", bc.frames)
	}
	tk := bc.frames[1].data.(map[string]any)
	if tk["lastPrice"] != "64510.1" || tk["priceSource"] != "trade" || tk["turnover24h"] != nil {
		t.Fatalf("ticker %#v", tk)
	}

	e.onOKX([]byte(`{"arg":{"channel":"books5","instId":"BTC-USDT-SWAP"},"data":[{"bids":[["64510","1","0","1"],["0","0","0","0"]],"asks":[["64510.5","2","0","1"]]}]}`), by, bc)
	d, ok := e.depth("BTCUSDT", 5)
	if !ok || len(d.Bids) != 1 || d.Bids[0][0] != "64510" || len(d.Asks) != 1 || !d.Snapshot {
		t.Fatalf("book %+v ok %v", d, ok)
	}
	if bc.count("futures-depth:BTCUSDT") != 1 {
		t.Fatalf("depth frames %d", bc.count("futures-depth:BTCUSDT"))
	}

	e.onOKX([]byte(`{"arg":{"channel":"candle1m","instId":"BTC-USDT-SWAP"},"data":[["1794820000000","64500","64520","64480","64510","10","0.2","12900","0"]]}`), by, bc)
	if bc.count("futures-kline:BTCUSDT:1m") != 1 {
		t.Fatalf("kline frames %d", bc.count("futures-kline:BTCUSDT:1m"))
	}
	k := bc.frames[len(bc.frames)-1].data.(map[string]any)
	if k["close"] != "64510" || k["interval"] != "1m" || k["volume"] != "0.2" {
		t.Fatalf("kline %#v", k)
	}
}

func TestBinanceCombinedURL(t *testing.T) {
	u := binanceCombinedURL("wss://data-stream.binance.vision", []streamSym{{local: "BTCUSDT", binance: "BTCUSDT"}}, false)
	if u != "wss://data-stream.binance.vision/stream?streams=btcusdt@trade/btcusdt@miniTicker/btcusdt@depth20@100ms/btcusdt@kline_1m/btcusdt@kline_15m/btcusdt@kline_1h/btcusdt@kline_4h/btcusdt@kline_1d/btcusdt@kline_1w" {
		t.Fatalf("url %s", u)
	}
}

// The swap book is incremental, sized in contracts, and must not leak into the
// spot book that /market/depth serves by default.
func TestOKXIncrementalBookInCoins(t *testing.T) {
	e := newExtFeed("https://api.binance.com", "https://fapi.binance.com", "https://www.okx.com")
	e.okxCt["BTC-USDT-SWAP"] = decimal.RequireFromString("0.01")
	bc := &recBroadcaster{}
	sym := streamSym{local: "BTCUSDT", binance: "BTCUSDT", fut: true, spot: true}
	by := map[string][]streamSym{"BTC-USDT-SWAP": {sym}}

	// Spot book from Binance, in coins.
	e.onDepth(streamSym{local: "BTCUSDT", binance: "BTCUSDT", spot: true}, []byte(`{"bids":[["65000.00","1.5"]],"asks":[["65000.01","2.5"]]}`), bc)

	snap := `{"arg":{"channel":"books","instId":"BTC-USDT-SWAP"},"action":"snapshot","data":[{"bids":[["64999.9","150","0","3"],["64999.8","20","0","1"]],"asks":[["65000","300","0","2"],["65000.1","40","0","1"]],"seqId":10,"prevSeqId":-1}]}`
	if e.onOKX([]byte(snap), by, bc) {
		t.Fatal("snapshot asked for a resync")
	}
	fut, ok := e.futDepth("BTCUSDT", 20)
	if !ok || len(fut.Bids) != 2 || fut.Bids[0] != [2]string{"64999.9", "1.5"} || fut.Asks[0] != [2]string{"65000", "3"} {
		t.Fatalf("swap book %+v ok %v", fut, ok)
	}
	spot, ok := e.depth("BTCUSDT", 20)
	if !ok || spot.Bids[0] != [2]string{"65000.00", "1.5"} || spot.Asks[0] != [2]string{"65000.01", "2.5"} {
		t.Fatalf("spot book was overwritten by the swap book: %+v", spot)
	}

	// Remove the best bid, change the best ask, add a better ask.
	e.now = func() time.Time { return time.Now().Add(time.Second) }
	upd := `{"arg":{"channel":"books","instId":"BTC-USDT-SWAP"},"action":"update","data":[{"bids":[["64999.9","0","0","0"]],"asks":[["65000","100","0","1"],["64999.95","5","0","1"]],"seqId":11,"prevSeqId":10}]}`
	if e.onOKX([]byte(upd), by, bc) {
		t.Fatal("in-sequence update asked for a resync")
	}
	fut, _ = e.futDepth("BTCUSDT", 20)
	if len(fut.Bids) != 1 || fut.Bids[0] != [2]string{"64999.8", "0.2"} {
		t.Fatalf("bids after update %+v", fut.Bids)
	}
	if len(fut.Asks) != 3 || fut.Asks[0] != [2]string{"64999.95", "0.05"} || fut.Asks[1] != [2]string{"65000", "1"} {
		t.Fatalf("asks after update %+v", fut.Asks)
	}
	if n := bc.count("futures-depth:BTCUSDT"); n != 2 {
		t.Fatalf("futures-depth frames %d", n)
	}

	// A gap in the sequence must reopen the socket instead of drifting.
	gap := `{"arg":{"channel":"books","instId":"BTC-USDT-SWAP"},"action":"update","data":[{"bids":[["64999.7","9","0","1"]],"asks":[],"seqId":20,"prevSeqId":15}]}`
	if !e.onOKX([]byte(gap), by, bc) {
		t.Fatal("out-of-sequence update was applied")
	}
	fut, _ = e.futDepth("BTCUSDT", 20)
	if len(fut.Bids) != 1 {
		t.Fatalf("gap update changed the book %+v", fut.Bids)
	}

	// Trades are converted too.
	e.onOKX([]byte(`{"arg":{"channel":"trades","instId":"BTC-USDT-SWAP"},"data":[{"tradeId":"7","px":"65000","sz":"12","side":"buy","ts":"1794820000000"}]}`), by, bc)
	var tr tradeDTO
	for _, f := range bc.frames {
		if f.channel == "futures-trades:BTCUSDT" {
			tr = f.data.(tradeDTO)
		}
	}
	if tr.Quantity != "0.12" {
		t.Fatalf("trade quantity %q, want coins", tr.Quantity)
	}
}

// Rapid updates are coalesced: one futures-depth frame per interval.
func TestOKXBookBroadcastThrottle(t *testing.T) {
	e := newExtFeed("", "", "")
	now := time.Unix(1794820000, 0)
	e.now = func() time.Time { return now }
	bc := &recBroadcaster{}
	by := map[string][]streamSym{"ETH-USDT-SWAP": {{local: "ETHUSDT", fut: true}}}
	e.onOKX([]byte(`{"arg":{"channel":"books","instId":"ETH-USDT-SWAP"},"action":"snapshot","data":[{"bids":[["1800","1","0","1"]],"asks":[["1801","1","0","1"]],"seqId":1,"prevSeqId":-1}]}`), by, bc)
	for i := 2; i <= 4; i++ {
		now = now.Add(40 * time.Millisecond)
		msg := fmt.Sprintf(`{"arg":{"channel":"books","instId":"ETH-USDT-SWAP"},"action":"update","data":[{"bids":[["1800","%d","0","1"]],"asks":[],"seqId":%d,"prevSeqId":%d}]}`, i, i, i-1)
		e.onOKX([]byte(msg), by, bc)
	}
	if n := bc.count("futures-depth:ETHUSDT"); n != 1 {
		t.Fatalf("frames inside the window %d, want 1", n)
	}
	now = now.Add(200 * time.Millisecond)
	e.onOKX([]byte(`{"arg":{"channel":"books","instId":"ETH-USDT-SWAP"},"action":"update","data":[{"bids":[["1800","9","0","1"]],"asks":[],"seqId":5,"prevSeqId":4}]}`), by, bc)
	d, _ := e.futDepth("ETHUSDT", 5)
	if bc.count("futures-depth:ETHUSDT") != 2 || d.Bids[0][1] != "9" {
		t.Fatalf("frames %d book %+v", bc.count("futures-depth:ETHUSDT"), d.Bids)
	}
}
