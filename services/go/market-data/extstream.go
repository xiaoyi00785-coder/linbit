package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/shopspring/decimal"
)

// extstream.go: Binance public websocket. Spot drives the spot page. USDⓈ-M
// drives the futures page, and also the spot page for symbols Binance only
// lists as perps (stocks, metals, oil).

const (
	// /ws accepts SUBSCRIBE. /stream only follows the ?streams= query and
	// ignores the subscribe message, so the futures feed stayed silent.
	spotWS = "wss://data-stream.binance.vision/ws"
	fapiWS = "wss://fstream.binance.com/ws"
)

var extPerpOnly = map[string]bool{
	"AAPLUSDT": true, "TSLAUSDT": true, "NVDAUSDT": true, "MSFTUSDT": true,
	"GOOGLUSDT": true, "AMZNUSDT": true, "METAUSDT": true, "COINUSDT": true,
	"SPYUSDT": true, "PAXGUSDT": true, "XAUTUSDT": true, "XAGUSDT": true, "WTIUSDT": true,
}

type streamSym struct {
	local   string
	binance string
	fut     bool // also publish futures-* channels
	spot    bool // publish ticker/depth/kline/trades
}

func (e *extFeed) stream(ctx context.Context, spotSyms []string, futURL string, bc Broadcaster) {
	if e == nil || bc == nil {
		return
	}
	// Combined ?streams= actually pushes. SUBSCRIBE on /ws was accepted and
	// then the book froze on the first snapshot.
	spot := spotBindings(spotSyms)
	go e.streamLoop(ctx, binanceCombinedURL("wss://data-stream.binance.vision", spot, false), spot, false, bc)
	go func() {
		var fut []streamSym
		for {
			if ctx.Err() != nil {
				return
			}
			fut = fapiBindings(spotSyms, loadContractSymbols(ctx, futURL))
			if len(fut) > 0 {
				break
			}
			log.Printf("ext stream: futures symbols not ready, retrying")
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
		// fstream accepts the socket and then sends nothing, so the futures
		// book, tape and candles froze on the last snapshot. OKX public
		// streams do deliver. The Binance REST poll still owns 24h turnover.
		go e.pollFAPITickers(ctx, fut, bc)
		go e.streamOKX(ctx, fut, bc)
		e.streamLoop(ctx, fapiWS, fut, true, bc)
	}()
}

// pollFAPITickers copies Binance USDⓈ-M 24h stats onto futures-ticker.
// One bulk request every 2s (weight 40) instead of a silent websocket.
func (e *extFeed) pollFAPITickers(ctx context.Context, syms []streamSym, bc Broadcaster) {
	if e == nil || e.fapi == "" || bc == nil {
		return
	}
	byBN := map[string][]streamSym{}
	for _, s := range syms {
		if s.fut && s.binance != "" {
			byBN[strings.ToUpper(s.binance)] = append(byBN[strings.ToUpper(s.binance)], s)
		}
	}
	if len(byBN) == 0 {
		return
	}
	logged := false
	wait := 2 * time.Second
	for {
		n, err := e.pullFAPITickers(ctx, byBN, bc)
		if err != nil && ctx.Err() == nil {
			log.Printf("ext fapi ticker: %v", err)
			if wait < 20*time.Second {
				wait *= 2
			}
		} else if err == nil {
			wait = 2 * time.Second
			if !logged {
				logged = true
				log.Printf("ext fapi ticker: %d contracts", n)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (e *extFeed) pullFAPITickers(ctx context.Context, byBN map[string][]streamSym, bc Broadcaster) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.fapi+"/fapi/v1/ticker/24hr", nil)
	if err != nil {
		return 0, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var rows []struct {
		Symbol string `json:"symbol"`
		Last   string `json:"lastPrice"`
		High   string `json:"highPrice"`
		Low    string `json:"lowPrice"`
		Vol    string `json:"volume"`
		Quote  string `json:"quoteVolume"`
		Pct    string `json:"priceChangePercent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		targets := byBN[strings.ToUpper(row.Symbol)]
		if len(targets) == 0 || row.Last == "" {
			continue
		}
		for _, s := range targets {
			e.publishVenueTicker(s, row.Last, row.High, row.Low, row.Vol, row.Quote, row.Pct, bc)
			n++
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("no matching contracts")
	}
	return n, nil
}

func (e *extFeed) publishVenueTicker(s streamSym, last, high, low, vol, quote, pct string, bc Broadcaster) {
	e.markLive(s.local)
	e.touchTicker(s.local, last, high, low, vol, quote, pct)
	if !s.fut || bc == nil {
		return
	}
	bc.Broadcast("futures-ticker:"+s.local, map[string]any{
		"symbol": s.local, "lastPrice": last, "high": high, "low": low,
		"highPrice24h": high, "lowPrice24h": low,
		"volume": vol, "volume24h": vol, "quoteVolume": quote, "turnover24h": quote,
		"priceChangePercent": pct, "priceChangePercent24h": pct, "priceSource": "trade",
	})
}

func spotBindings(spotSyms []string) []streamSym {
	var out []streamSym
	seen := map[string]bool{}
	for _, s := range spotSyms {
		s = strings.ToUpper(s)
		if s == "" || seen[s] || extPerpOnly[s] {
			continue
		}
		seen[s] = true
		out = append(out, streamSym{local: s, binance: s, spot: true})
	}
	return out
}

func fapiBindings(spotSyms, futSyms []string) []streamSym {
	var out []streamSym
	seen := map[string]bool{}
	add := func(s string, fut, spot bool) {
		s = strings.ToUpper(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		bn := s
		if alt := extFAPIAlias[s]; alt != "" {
			bn = alt
		}
		out = append(out, streamSym{local: s, binance: bn, fut: fut, spot: spot})
	}
	for _, s := range futSyms {
		add(s, true, extPerpOnly[strings.ToUpper(s)])
	}
	for _, s := range spotSyms {
		if extPerpOnly[strings.ToUpper(s)] {
			add(s, false, true)
		}
	}
	return out
}

func (e *extFeed) streamLoop(ctx context.Context, host string, syms []streamSym, fapi bool, bc Broadcaster) {
	if len(syms) == 0 {
		return
	}
	back := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := e.streamOnce(ctx, host, syms, fapi, bc)
		if err != nil && ctx.Err() == nil {
			label := host
			if i := strings.Index(host, "?"); i > 0 {
				label = host[:i]
			}
			log.Printf("ext stream %s: %v", label, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(back):
		}
		if back < 15*time.Second {
			back *= 2
		}
	}
}

func (e *extFeed) streamOnce(ctx context.Context, host string, syms []streamSym, fapi bool, bc Broadcaster) error {
	dial, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dial, host, nil)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(8 << 20)
	label := host
	if i := strings.Index(host, "?"); i > 0 {
		label = host[:i]
	}
	log.Printf("ext stream connected %s (%d symbols)", label, len(syms))
	byStream := map[string][]streamSym{}
	bySymbol := map[string][]streamSym{}
	var params []string
	for _, s := range syms {
		low := strings.ToLower(s.binance)
		bySymbol[strings.ToUpper(s.binance)] = append(bySymbol[strings.ToUpper(s.binance)], s)
		add := func(suffix string) {
			name := low + "@" + suffix
			params = append(params, name)
			byStream[name] = append(byStream[name], s)
		}
		if fapi {
			add("aggTrade")
		} else {
			add("trade")
		}
		add("miniTicker")
		add("depth20@100ms")
		for _, iv := range extChartIntervals {
			add("kline_" + iv)
		}
	}
	// A URL that already names ?streams= is subscribed. Sending SUBSCRIBE
	// there is ignored, which is how the book froze after one snapshot.
	if !strings.Contains(host, "streams=") {
		for i := 0; i < len(params); i += 80 {
			j := i + 80
			if j > len(params) {
				j = len(params)
			}
			body, _ := json.Marshal(map[string]any{"method": "SUBSCRIBE", "params": params[i:j], "id": i + 1})
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = conn.Write(wctx, websocket.MessageText, body)
			cancel()
			if err != nil {
				return err
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	idle := time.Duration(0)
	if strings.Contains(host, "streams=") {
		idle = 25 * time.Second
	}
	for {
		readCtx := ctx
		var cancel context.CancelFunc
		if idle > 0 {
			readCtx, cancel = context.WithTimeout(ctx, idle)
		}
		_, msg, err := conn.Read(readCtx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			return err
		}
		var env struct {
			Stream string          `json:"stream"`
			Data   json.RawMessage `json:"data"`
		}
		if json.Unmarshal(msg, &env) != nil {
			continue
		}
		if env.Stream != "" && len(env.Data) > 0 {
			for _, s := range byStream[env.Stream] {
				e.onStream(s, env.Stream, env.Data, bc)
			}
			continue
		}
		// /ws delivers the event itself, with no stream wrapper.
		e.onRaw(msg, bySymbol, bc)
	}
}

func (e *extFeed) onRaw(msg []byte, bySymbol map[string][]streamSym, bc Broadcaster) {
	var head struct {
		Event string `json:"e"`
		Sym   string `json:"s"`
	}
	if json.Unmarshal(msg, &head) != nil || head.Sym == "" {
		return
	}
	name := strings.ToLower(head.Sym) + "@" + head.Event
	for _, s := range bySymbol[strings.ToUpper(head.Sym)] {
		e.onStream(s, name, msg, bc)
	}
}

func (e *extFeed) onStream(s streamSym, name string, raw json.RawMessage, bc Broadcaster) {
	switch {
	case strings.Contains(name, "@trade") || strings.Contains(name, "@aggTrade"):
		e.onTrade(s, raw, bc)
	case strings.Contains(name, "MiniTicker") || strings.Contains(name, "@miniTicker"):
		e.onMini(s, raw, bc)
	case strings.Contains(name, "@kline"):
		e.onKline(s, name, raw, bc)
	case strings.Contains(name, "@depth"):
		e.onDepth(s, raw, bc)
	}
}

func (e *extFeed) onTrade(s streamSym, raw json.RawMessage, bc Broadcaster) {
	var ev struct {
		T  int64  `json:"T"`
		P  string `json:"p"`
		Q  string `json:"q"`
		M  bool   `json:"m"`
		A  int64  `json:"a"`
		ID int64  `json:"t"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.P == "" {
		return
	}
	id := ev.ID
	if ev.A != 0 {
		id = ev.A
	}
	side := "buy"
	if ev.M {
		side = "sell"
	}
	tr := tradeDTO{TradeID: decimal.NewFromInt(id).String(), Price: ev.P, Quantity: ev.Q, Side: side, Time: ev.T}
	e.markLive(s.local)
	e.touchTicker(s.local, ev.P, "", "", "", "", "")
	if s.spot {
		bc.Broadcast("trades:"+s.local, tr)
		if tk, ok := e.ticker(s.local); ok {
			bc.Broadcast("ticker:"+s.local, tk)
		}
	}
	if s.fut {
		bc.Broadcast("futures-trades:"+s.local, tr)
		bc.Broadcast("futures-ticker:"+s.local, map[string]any{
			"symbol": s.local, "lastPrice": ev.P, "priceSource": "trade",
		})
	}
}

func (e *extFeed) onMini(s streamSym, raw json.RawMessage, bc Broadcaster) {
	var ev struct {
		C string `json:"c"`
		O string `json:"o"`
		H string `json:"h"`
		L string `json:"l"`
		V string `json:"v"`
		Q string `json:"q"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.C == "" {
		return
	}
	pct := ""
	o, err1 := decimal.NewFromString(ev.O)
	c, err2 := decimal.NewFromString(ev.C)
	if err1 == nil && err2 == nil && o.IsPositive() {
		pct = c.Sub(o).Div(o).Mul(decimal.NewFromInt(100)).Round(2).String()
	}
	e.publishVenueTicker(s, ev.C, ev.H, ev.L, ev.V, ev.Q, pct, bc)
	if s.spot {
		if tk, ok := e.ticker(s.local); ok {
			bc.Broadcast("ticker:"+s.local, tk)
		}
	}
}

func (e *extFeed) onKline(s streamSym, name string, raw json.RawMessage, bc Broadcaster) {
	var ev struct {
		K struct {
			T int64  `json:"t"`
			O string `json:"o"`
			H string `json:"h"`
			L string `json:"l"`
			C string `json:"c"`
			V string `json:"v"`
			X bool   `json:"x"`
			I string `json:"i"`
		} `json:"k"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.K.C == "" {
		return
	}
	iv := ev.K.I
	if iv == "" {
		if i := strings.Index(name, "@kline_"); i >= 0 {
			iv = name[i+len("@kline_"):]
		}
	}
	k := wsKline{Time: ev.K.T, Open: ev.K.O, High: ev.K.H, Low: ev.K.L, Close: ev.K.C, Volume: ev.K.V, Closed: ev.K.X}
	e.markLive(s.local)
	if s.spot {
		bc.Broadcast("kline:"+s.local+":"+iv, k)
	}
	if s.fut {
		bc.Broadcast("futures-kline:"+s.local+":"+iv, map[string]any{
			"symbol": s.local, "interval": iv, "time": ev.K.T, "openTime": ev.K.T,
			"open": ev.K.O, "high": ev.K.H, "low": ev.K.L, "close": ev.K.C,
			"volume": ev.K.V, "closed": ev.K.X,
		})
	}
}

func (e *extFeed) onDepth(s streamSym, raw json.RawMessage, bc Broadcaster) {
	var ev struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
		B    [][]string `json:"b"`
		A    [][]string `json:"a"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	bids, asks := ev.Bids, ev.Asks
	if len(bids) == 0 {
		bids = ev.B
	}
	if len(asks) == 0 {
		asks = ev.A
	}
	if len(bids) == 0 && len(asks) == 0 {
		return
	}
	e.mu.Lock()
	id := e.seq[s.local] + 1
	e.seq[s.local] = id
	d := depthDTO{Symbol: s.local, LastUpdateID: id, Bids: pairLevels(bids), Asks: pairLevels(asks), Snapshot: true}
	// A futures binding feeds the swap book. Only a spot binding may touch
	// the spot book.
	if s.spot || !s.fut {
		e.books[s.local] = d
	}
	if s.fut {
		e.futBooks[s.local] = d
	}
	e.mu.Unlock()
	e.markLive(s.local)
	if s.spot {
		bc.Broadcast("depth:"+s.local, d)
	}
	if s.fut {
		bc.Broadcast("futures-depth:"+s.local, d)
	}
}

func pairLevels(rows [][]string) [][2]string {
	out := make([][2]string, 0, len(rows))
	for _, r := range rows {
		if len(r) < 2 || r[1] == "0" || r[1] == "0.00000000" {
			continue
		}
		out = append(out, [2]string{r[0], r[1]})
	}
	return out
}

func (e *extFeed) touchTicker(symbol, last, high, low, vol, quote, pct string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ticks == nil {
		e.ticks = map[string]tickerDTO{}
	}
	tk := e.ticks[symbol]
	tk.Symbol = symbol
	if last != "" {
		tk.LastPrice = last
	}
	if high != "" {
		tk.High = high
	}
	if low != "" {
		tk.Low = low
	}
	if vol != "" {
		tk.Volume = vol
	}
	if quote != "" {
		tk.QuoteVolume = quote
	}
	if pct != "" {
		tk.PriceChangePercent = pct
	}
	tk.PriceSource = "trade"
	e.ticks[symbol] = tk
}

func (e *extFeed) ticker(symbol string) (tickerDTO, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	tk, ok := e.ticks[symbol]
	return tk, ok && tk.LastPrice != ""
}

func binanceCombinedURL(base string, syms []streamSym, fapi bool) string {
	var parts []string
	for _, s := range syms {
		low := strings.ToLower(s.binance)
		if low == "" {
			continue
		}
		if fapi {
			parts = append(parts, low+"@aggTrade")
		} else {
			parts = append(parts, low+"@trade")
		}
		parts = append(parts, low+"@miniTicker", low+"@depth20@100ms")
		for _, iv := range extChartIntervals {
			parts = append(parts, low+"@kline_"+iv)
		}
	}
	if len(parts) == 0 {
		return strings.TrimRight(base, "/") + "/ws"
	}
	return strings.TrimRight(base, "/") + "/stream?streams=" + strings.Join(parts, "/")
}

const okxPublicWS = "wss://ws.okx.com/ws/v5/public"

func okxSwapInst(local string) string {
	switch strings.ToUpper(local) {
	case "PAXGUSDT", "XAUTUSDT":
		return "XAU-USDT-SWAP"
	default:
		return okxInst(local, true)
	}
}

func okxCandleInterval(ch string) string {
	switch ch {
	case "candle1m":
		return "1m"
	case "candle15m":
		return "15m"
	case "candle1H":
		return "1h"
	case "candle4H":
		return "4h"
	case "candle1D":
		return "1d"
	case "candle1W":
		return "1w"
	default:
		return ""
	}
}

// streamOKX pushes futures trades, the top of book and candles. Binance
// USDⓈ-M websocket sends nothing from this host; OKX does. Candles live on
// the business socket; trades and books live on the public socket.
func (e *extFeed) streamOKX(ctx context.Context, syms []streamSym, bc Broadcaster) {
	var fut []streamSym
	for _, s := range syms {
		if s.fut {
			fut = append(fut, s)
		}
	}
	if len(fut) == 0 {
		return
	}
	go e.loadOKXContractValues(ctx)
	go e.streamOKXLoop(ctx, okxPublicWS, []string{"trades", okxBookChannel()}, fut, bc)
	e.streamOKXLoop(ctx, "wss://ws.okx.com/ws/v5/business", []string{"candle1m", "candle15m", "candle1H", "candle4H", "candle1D", "candle1W"}, fut, bc)
}

func (e *extFeed) streamOKXLoop(ctx context.Context, url string, channels []string, syms []streamSym, bc Broadcaster) {
	back := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		err := e.streamOKXOnce(ctx, url, channels, syms, bc)
		if err != nil && ctx.Err() == nil {
			log.Printf("ext okx stream %s: %v", url, err)
		}
		if time.Since(started) > 30*time.Second {
			back = time.Second // a connection that worked earns a fast reconnect
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(back):
		}
		if back < 15*time.Second {
			back *= 2
		}
	}
}

func (e *extFeed) streamOKXOnce(ctx context.Context, url string, channels []string, syms []streamSym, bc Broadcaster) error {
	dial, cancelDial := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dial, url, nil)
	cancelDial()
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(8 << 20)
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				wctx, c := context.WithTimeout(sctx, 5*time.Second)
				werr := conn.Write(wctx, websocket.MessageText, []byte("ping"))
				c()
				if werr != nil {
					return
				}
			}
		}
	}()
	byInst := map[string][]streamSym{}
	for _, s := range syms {
		inst := okxSwapInst(s.local)
		byInst[inst] = append(byInst[inst], s)
	}
	var args []map[string]string
	for inst := range byInst {
		for _, ch := range channels {
			args = append(args, map[string]string{"channel": ch, "instId": inst})
		}
	}
	for i := 0; i < len(args); i += 40 {
		j := i + 40
		if j > len(args) {
			j = len(args)
		}
		body, _ := json.Marshal(map[string]any{"op": "subscribe", "args": args[i:j]})
		wctx, c := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Write(wctx, websocket.MessageText, body)
		c()
		if err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("ext okx stream connected %s (%d symbols)", url, len(byInst))
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if e.onOKX(msg, byInst, bc) {
			return errOKXBookGap
		}
	}
}

var errOKXBookGap = fmt.Errorf("okx book update out of sequence, resubscribing")

// onOKX handles one OKX frame. It reports true when an incremental book lost
// its sequence and the socket has to be reopened for a fresh snapshot.
func (e *extFeed) onOKX(msg []byte, byInst map[string][]streamSym, bc Broadcaster) (resync bool) {
	if string(msg) == "pong" {
		return
	}
	var env struct {
		Event  string `json:"event"`
		Msg    string `json:"msg"`
		Action string `json:"action"`
		Arg    struct {
			Channel string `json:"channel"`
			InstID  string `json:"instId"`
		} `json:"arg"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(msg, &env) != nil || env.Event != "" {
		if env.Event == "error" && env.Msg != "" {
			log.Printf("ext okx: %s", env.Msg)
		}
		return
	}
	targets := byInst[env.Arg.InstID]
	if len(targets) == 0 || len(env.Data) == 0 {
		return
	}
	switch {
	case env.Arg.Channel == "trades":
		e.onOKXTrades(env.Arg.InstID, targets, env.Data, bc)
	case strings.HasPrefix(env.Arg.Channel, "books"):
		return e.onOKXBooks(env.Arg.InstID, env.Action, targets, env.Data, bc)
	case strings.HasPrefix(env.Arg.Channel, "candle"):
		e.onOKXCandle(targets, okxCandleInterval(env.Arg.Channel), env.Data, bc)
	}
	return false
}

func (e *extFeed) onOKXTrades(inst string, targets []streamSym, raw json.RawMessage, bc Broadcaster) {
	var rows []struct {
		TradeID string `json:"tradeId"`
		Px      string `json:"px"`
		Sz      string `json:"sz"`
		Side    string `json:"side"`
		Ts      string `json:"ts"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	for _, row := range rows {
		if row.Px == "" {
			continue
		}
		ts, _ := strconv.ParseInt(row.Ts, 10, 64)
		side := row.Side
		if side != "sell" {
			side = "buy"
		}
		// OKX sizes are in contracts. The app shows coins.
		tr := tradeDTO{TradeID: row.TradeID, Price: row.Px, Quantity: e.okxCoins(inst, row.Sz), Side: side, Time: ts}
		for _, s := range targets {
			e.markLive(s.local)
			e.touchTicker(s.local, row.Px, "", "", "", "", "")
			if bc == nil {
				continue
			}
			bc.Broadcast("futures-trades:"+s.local, tr)
			// Last price only. 24h turnover stays on the Binance REST poll.
			bc.Broadcast("futures-ticker:"+s.local, map[string]any{
				"symbol": s.local, "lastPrice": row.Px, "priceSource": "trade",
			})
		}
	}
}

// okxBookChannel picks the OKX depth channel. "books5" (default) is five
// levels a side and about 4 KB/s per symbol. "books" is the 400-level
// incremental book, which gives the trade page 20 levels but measured
// 100-300 KB/s for 12 symbols. The mainnet host is capped near 600 KB/s
// inbound and already runs at that cap, so the deep book is opt-in:
// OKX_BOOK_CHANNEL=books.
func okxBookChannel() string {
	if strings.TrimSpace(os.Getenv("OKX_BOOK_CHANNEL")) == "books" {
		return "books"
	}
	return "books5"
}

// okxBook is the local copy of one OKX swap book, sizes in contracts.
type okxBook struct {
	bids, asks map[string]string // price -> size, both as OKX sent them
	seq        int64
	ready      bool
}

const (
	okxBookLevels   = 20                     // levels a side sent to clients
	okxBookInterval = 150 * time.Millisecond // at most one futures-depth frame per instrument in this window
)

// onOKXBooks keeps the swap book and broadcasts its top levels in coins.
// Channel "books" sends one snapshot and then updates (size "0" removes a
// level). "books5" and other snapshot-only channels have no action field and
// replace the book on every frame.
func (e *extFeed) onOKXBooks(inst, action string, targets []streamSym, raw json.RawMessage, bc Broadcaster) (resync bool) {
	var rows []struct {
		Bids      [][]string `json:"bids"`
		Asks      [][]string `json:"asks"`
		SeqID     int64      `json:"seqId"`
		PrevSeqID int64      `json:"prevSeqId"`
	}
	if json.Unmarshal(raw, &rows) != nil || len(rows) == 0 {
		return false
	}
	row := rows[0]
	e.mu.Lock()
	if e.okxBooks == nil {
		e.okxBooks = map[string]*okxBook{}
	}
	b := e.okxBooks[inst]
	if b == nil {
		b = &okxBook{}
		e.okxBooks[inst] = b
	}
	snapshot := action != "update"
	switch {
	case snapshot:
		b.bids, b.asks, b.ready = map[string]string{}, map[string]string{}, true
	case !b.ready:
		e.mu.Unlock()
		return false // update before the snapshot: wait for it
	case row.PrevSeqID != b.seq:
		b.ready = false
		e.mu.Unlock()
		return true
	}
	applyOKXLevels(b.bids, row.Bids)
	applyOKXLevels(b.asks, row.Asks)
	b.seq = row.SeqID
	// Snapshot-only channels push ten frames a second. Clients redraw twice a
	// second, so one frame per window is enough either way.
	now := e.now()
	if now.Sub(e.okxEmit[inst]) < okxBookInterval {
		e.mu.Unlock()
		return false
	}
	if e.okxEmit == nil {
		e.okxEmit = map[string]time.Time{}
	}
	e.okxEmit[inst] = now
	ct, hasCt := e.okxCt[inst]
	bids := okxTopLevels(b.bids, true, okxBookLevels, ct, hasCt)
	asks := okxTopLevels(b.asks, false, okxBookLevels, ct, hasCt)
	e.mu.Unlock()
	if len(bids) == 0 && len(asks) == 0 {
		return false
	}
	for _, s := range targets {
		e.mu.Lock()
		if e.seq == nil {
			e.seq = map[string]uint64{}
		}
		id := e.seq[s.local] + 1
		e.seq[s.local] = id
		d := depthDTO{Symbol: s.local, LastUpdateID: id, Bids: bids, Asks: asks, Snapshot: true}
		if e.futBooks == nil {
			e.futBooks = map[string]depthDTO{}
		}
		e.futBooks[s.local] = d
		e.mu.Unlock()
		e.markLive(s.local)
		if bc != nil {
			bc.Broadcast("futures-depth:"+s.local, d)
		}
	}
	return false
}

func applyOKXLevels(side map[string]string, rows [][]string) {
	for _, r := range rows {
		if len(r) < 2 || r[0] == "" {
			continue
		}
		if sz, err := strconv.ParseFloat(r[1], 64); err != nil || sz <= 0 {
			delete(side, r[0])
			continue
		}
		side[r[0]] = r[1]
	}
}

// okxTopLevels returns the best n levels, sizes converted from contracts to
// coins when the contract value is known.
func okxTopLevels(side map[string]string, bids bool, n int, ct decimal.Decimal, hasCt bool) [][2]string {
	type lvl struct {
		px    float64
		p, sz string
	}
	all := make([]lvl, 0, len(side))
	for p, sz := range side {
		f, err := strconv.ParseFloat(p, 64)
		if err != nil || f <= 0 {
			continue
		}
		all = append(all, lvl{f, p, sz})
	}
	sort.Slice(all, func(i, j int) bool {
		if bids {
			return all[i].px > all[j].px
		}
		return all[i].px < all[j].px
	})
	if len(all) > n {
		all = all[:n]
	}
	out := make([][2]string, 0, len(all))
	for _, l := range all {
		sz := l.sz
		if hasCt {
			if d, err := decimal.NewFromString(l.sz); err == nil {
				sz = d.Mul(ct).String()
			}
		}
		out = append(out, [2]string{l.p, sz})
	}
	return out
}

// okxCoins converts an OKX contract count to coins. Unknown instruments pass
// through unchanged.
func (e *extFeed) okxCoins(inst, sz string) string {
	e.mu.Lock()
	ct, ok := e.okxCt[inst]
	e.mu.Unlock()
	if !ok {
		return sz
	}
	d, err := decimal.NewFromString(sz)
	if err != nil {
		return sz
	}
	return d.Mul(ct).String()
}

// loadOKXContractValues reads ctVal for every swap and refreshes it hourly.
// Until the first load succeeds sizes go out in contracts, as they did before.
func (e *extFeed) loadOKXContractValues(ctx context.Context) {
	wait := 5 * time.Second
	for {
		if err := e.pullOKXContractValues(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("ext okx: contract values: %v", err)
		} else {
			wait = time.Hour
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (e *extFeed) pullOKXContractValues(ctx context.Context) error {
	var body struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID string `json:"instId"`
			CtVal  string `json:"ctVal"`
		} `json:"data"`
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := getJSONLimit(rctx, e.okx+"/api/v5/public/instruments?instType=SWAP", 32<<20, &body); err != nil {
		return err
	}
	if body.Code != "0" && body.Code != "" {
		return fmt.Errorf("okx instruments: %s %s", body.Code, body.Msg)
	}
	vals := map[string]decimal.Decimal{}
	for _, r := range body.Data {
		if d, err := decimal.NewFromString(r.CtVal); err == nil && d.IsPositive() {
			vals[r.InstID] = d
		}
	}
	if len(vals) == 0 {
		return fmt.Errorf("okx instruments: empty list")
	}
	e.mu.Lock()
	e.okxCt = vals
	e.mu.Unlock()
	log.Printf("ext okx: loaded %d swap contract values", len(vals))
	return nil
}

func (e *extFeed) onOKXCandle(targets []streamSym, interval string, raw json.RawMessage, bc Broadcaster) {
	if interval == "" || bc == nil {
		return
	}
	var rows [][]string
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	for _, row := range rows {
		if len(row) < 6 || row[4] == "" {
			continue
		}
		ts, _ := strconv.ParseInt(row[0], 10, 64)
		vol := row[5]
		if len(row) > 6 && row[6] != "" {
			vol = row[6]
		}
		closed := len(row) > 8 && row[8] == "1"
		for _, s := range targets {
			e.markLive(s.local)
			bc.Broadcast("futures-kline:"+s.local+":"+interval, map[string]any{
				"symbol": s.local, "interval": interval, "time": ts, "openTime": ts,
				"open": row[1], "high": row[2], "low": row[3], "close": row[4],
				"volume": vol, "closed": closed,
			})
		}
	}
}

func loadContractSymbols(ctx context.Context, base string) []string {
	if strings.TrimSpace(base) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var body struct {
		Contracts []struct {
			Symbol string `json:"symbol"`
		} `json:"contracts"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/futures/market/contracts", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return nil
	}
	out := make([]string, 0, len(body.Contracts))
	for _, c := range body.Contracts {
		if c.Symbol != "" {
			out = append(out, c.Symbol)
		}
	}
	return out
}
