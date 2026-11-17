package main

import "testing"

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
