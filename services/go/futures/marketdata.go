package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// marketdata.go: public futures market data.

func (s *Server) hContracts(w http.ResponseWriter, r *http.Request) {
	tags := loadSymbolTags(r.Context(), s.db)
	out := make([]Contract, 0, len(s.symbols))
	for _, sym := range s.symbols {
		c := s.contracts[sym]
		c.Tags = tags[sym]
		if c.Tags == nil {
			c.Tags = []string{}
		}
		out = append(out, c)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"contracts": out, "defaults": map[string]any{
		"leverage": s.cfg.DefaultLeverage, "marginMode": s.cfg.DefaultMarginMode, "positionMode": "one_way"}})
}

func (s *Server) ticker(r *http.Request, sym string) map[string]any {
	ps := s.snapshot(sym)
	var open, high, low, vol, turn decimal.NullDecimal
	_ = s.db.QueryRowContext(r.Context(), `SELECT (array_agg(price ORDER BY id))[1], max(price), min(price), sum(qty), sum(qty*price)
		FROM fut_trades WHERE symbol=$1 AND created_at > now() - interval '24 hours'`, sym).Scan(&open, &high, &low, &vol, &turn)
	var oi decimal.Decimal
	_ = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(sum(qty),0) FROM fut_positions WHERE symbol=$1 AND qty > 0`, sym).Scan(&oi)
	last, src := s.displayPrice(r, sym, ps)
	change := dZero
	if open.Valid && open.Decimal.Sign() > 0 && last.Sign() > 0 {
		change = last.Sub(open.Decimal).Div(open.Decimal).Round(6)
	}
	// No trades in 24h: open/high/low collapse to the display price so the
	// UI shows a flat 0% day rather than 0 prices / nonsense percentages.
	z := func(n decimal.NullDecimal) decimal.Decimal {
		if n.Valid {
			return n.Decimal
		}
		return last
	}
	zv := func(n decimal.NullDecimal) decimal.Decimal {
		if n.Valid {
			return n.Decimal
		}
		return dZero
	}
	return map[string]any{
		"symbol": sym, "lastPrice": last, "priceSource": src, "markPrice": ps.Mark, "indexPrice": ps.Index,
		"fundingRate": s.predictedRate(ps), "lastFundingRate": ps.LastRate,
		"nextFundingTime": s.nextFunding(s.now()).UnixMilli(),
		"openPrice24h":    z(open), "highPrice24h": z(high), "lowPrice24h": z(low),
		"volume24h": zv(vol), "turnover24h": money(zv(turn)), "priceChangePercent24h": change,
		"openInterest": oi, "priceAvailable": ps.Mark.Sign() > 0,
		// Spot-ticker-compatible aliases so the UI can share one ticker shape.
		"high": z(high), "low": z(low), "quoteVolume": money(zv(turn)), "volume": zv(vol),
		"priceChangePercent": change.Mul(decimal.NewFromInt(100)).Round(2),
	}
}

func (s *Server) hTicker(w http.ResponseWriter, r *http.Request) {
	if sym := r.URL.Query().Get("symbol"); sym != "" {
		c, err := s.contract(sym)
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, s.ticker(r, c.Symbol))
		return
	}
	out := []map[string]any{}
	for _, sym := range s.symbols {
		out = append(out, s.ticker(r, sym))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"tickers": out})
}

func (s *Server) hDepth(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	n := limitParam(r, 20, 200)
	side := func(sd, ord string) [][2]decimal.Decimal {
		rows, err := s.db.QueryContext(r.Context(), `SELECT price, sum(qty-filled) FROM fut_orders WHERE symbol=$1 AND side=$2 AND type='limit'
			AND status IN ('new','partially_filled') GROUP BY price ORDER BY price `+ord+` LIMIT $3`, c.Symbol, sd, n)
		out := [][2]decimal.Decimal{}
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var p, q decimal.Decimal
			if rows.Scan(&p, &q) == nil {
				out = append(out, [2]decimal.Decimal{p, q})
			}
		}
		return out
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "bids": side("buy", "DESC"), "asks": side("sell", "ASC"), "time": s.now().UnixMilli()})
}

func (s *Server) hTrades(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, price, qty, taker_side, created_at FROM fut_trades WHERE symbol=$1 ORDER BY id DESC LIMIT $2`, c.Symbol, limitParam(r, 50, 500))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []Trade{}
	for rows.Next() {
		t := Trade{Symbol: c.Symbol}
		if rows.Scan(&t.ID, &t.Price, &t.Qty, &t.TakerSide, &t.Time) == nil {
			out = append(out, t)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"trades": out})
}

var klineSec = map[string]int64{"1m": 60, "5m": 300, "15m": 900, "30m": 1800, "1h": 3600, "4h": 14400, "1d": 86400, "1w": 604800}

// klineAlias accepts the chart's upper-case labels (1H/4H/1D/1W).
var klineAlias = map[string]string{"1H": "1h", "4H": "4h", "1D": "1d", "1W": "1w"}

// klineAnchor aligns weekly buckets to Monday 00:00 UTC (same as market-data).
func klineAnchor(sec int64) int64 {
	if sec == 604800 {
		return 345600 // 1970-01-05T00:00:00Z
	}
	return 0
}

func (s *Server) hKlines(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	iv := strings.TrimSpace(r.URL.Query().Get("interval"))
	if iv == "" {
		iv = "1m"
	}
	if a, ok := klineAlias[iv]; ok {
		iv = a
	}
	sec, ok := klineSec[iv]
	if !ok {
		writeErr(w, bad("VALIDATION_ERROR", "interval 仅支持 1m/5m/15m/30m/1h/4h/1d/1w"))
		return
	}
	n := limitParam(r, 200, 1000)
	anchor := klineAnchor(sec)
	rows, err := s.db.QueryContext(r.Context(), `SELECT b, (array_agg(price ORDER BY id))[1], max(price), min(price), (array_agg(price ORDER BY id DESC))[1], sum(qty), sum(qty*price)
		FROM (SELECT id, price, qty, (floor((extract(epoch from created_at) - $4) / $2) * $2 + $4)::bigint AS b FROM fut_trades
		      WHERE symbol=$1 AND created_at >= now() - make_interval(secs => $2 * $3)) t
		GROUP BY b ORDER BY b`, c.Symbol, sec, n, anchor)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	trade := []klineBar{}
	for rows.Next() {
		var b klineBar
		if rows.Scan(&b.Open, &b.O, &b.H, &b.L, &b.C, &b.V, &b.T) == nil {
			trade = append(trade, b)
		}
	}
	rows.Close()
	// Index candles fill every bucket without futures trades, so a thin
	// market still has continuous history (MA on 4h/1d/1w, a real 1w chart).
	// Preferred: market-data's index-backed klines (Binance-backfilled
	// history); fallback: fut_mark_history samples.
	idx, _ := s.market.Klines(r.Context(), c.Symbol, iv, n)
	if len(idx) == 0 {
		idx = s.indexKlines(r, c.Symbol, sec, n, anchor)
	}
	out, source := mergeFutKlines(trade, idx, n)
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "interval": iv, "source": source, "klines": out})
}

// klineBar is one candle; Open is the bucket open time in unix seconds.
type klineBar struct {
	Open          int64
	O, H, L, C, V decimal.Decimal
	T             decimal.Decimal
	Index         bool
}

// mergeFutKlines unions trade and index candles by bucket (trade wins), keeps
// the newest n, and reports source: trade | index | mixed | none.
func mergeFutKlines(trade, idx []klineBar, n int) ([]map[string]any, string) {
	byT := map[int64]klineBar{}
	for _, b := range idx {
		b.Index, b.V, b.T = true, dZero, dZero
		byT[b.Open] = b
	}
	for _, b := range trade {
		b.Index = false
		byT[b.Open] = b
	}
	keys := make([]int64, 0, len(byT))
	for k := range byT {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if n > 0 && len(keys) > n {
		keys = keys[len(keys)-n:]
	}
	out := make([]map[string]any, 0, len(keys))
	hasT, hasI := false, false
	for _, k := range keys {
		b := byT[k]
		m := map[string]any{"openTime": b.Open * 1000, "open": b.O, "high": b.H, "low": b.L, "close": b.C, "volume": b.V, "turnover": money(b.T)}
		if b.Index {
			m["source"] = "index"
			hasI = true
		} else {
			hasT = true
		}
		out = append(out, m)
	}
	switch {
	case hasT && hasI:
		return out, "mixed"
	case hasT:
		return out, "trade"
	case hasI:
		return out, "index"
	}
	return out, "none"
}

func (s *Server) hInsurance(w http.ResponseWriter, r *http.Request) {
	bal, _, err := readAccount(r.Context(), s.db, insuranceUser)
	if err != nil {
		writeErr(w, err)
		return
	}
	st, _ := s.state(r.Context(), s.db, insuranceUser)
	rows, err := s.db.QueryContext(r.Context(), `SELECT symbol, margin_mode, qty, mark_price, fee, shortfall, created_at FROM fut_liquidations ORDER BY id DESC LIMIT $1`, limitParam(r, 50, 500))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	liqs := []map[string]any{}
	for rows.Next() {
		var sym, mode string
		var q, m, f, sf decimal.Decimal
		var at time.Time
		if rows.Scan(&sym, &mode, &q, &m, &f, &sf, &at) == nil {
			liqs = append(liqs, map[string]any{"symbol": sym, "marginMode": mode, "qty": q, "markPrice": m, "fee": f, "shortfall": sf, "time": at.UnixMilli()})
		}
	}
	adl := []map[string]any{}
	if ar, err := s.db.QueryContext(r.Context(), `SELECT symbol, qty, price, created_at FROM fut_adl ORDER BY id DESC LIMIT 50`); err == nil {
		for ar.Next() {
			var sym string
			var q, px decimal.Decimal
			var at time.Time
			if ar.Scan(&sym, &q, &px, &at) == nil {
				adl = append(adl, map[string]any{"symbol": sym, "qty": q, "price": px, "time": at.UnixMilli()})
			}
		}
		ar.Close()
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"asset": "USDT", "balance": money(bal), "equity": money(st.Equity), "positions": st.Positions, "recentLiquidations": liqs, "recentAdl": adl})
}

// displayPrice: last trade, else mid of best bid/ask, else index, else mark.
func (s *Server) displayPrice(r *http.Request, sym string, ps priceState) (decimal.Decimal, string) {
	if ps.Last.Sign() > 0 {
		return ps.Last, "trade"
	}
	var bid, ask decimal.NullDecimal
	_ = s.db.QueryRowContext(r.Context(), `SELECT
		(SELECT max(price) FROM fut_orders WHERE symbol=$1 AND side='buy' AND type='limit' AND status IN ('new','partially_filled')),
		(SELECT min(price) FROM fut_orders WHERE symbol=$1 AND side='sell' AND type='limit' AND status IN ('new','partially_filled'))`, sym).Scan(&bid, &ask)
	if bid.Valid && ask.Valid && bid.Decimal.Sign() > 0 && ask.Decimal.GreaterThanOrEqual(bid.Decimal) {
		return bid.Decimal.Add(ask.Decimal).Div(decimal.NewFromInt(2)), "mid"
	}
	if ps.Index.Sign() > 0 {
		return ps.Index, "index"
	}
	if ps.Mark.Sign() > 0 {
		return ps.Mark, "mark"
	}
	return dZero, "none"
}

// indexKlines builds OHLC candles from the recorded index price history.
func (s *Server) indexKlines(r *http.Request, sym string, sec int64, n int, anchor int64) []klineBar {
	out := []klineBar{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT b, (array_agg(index_price ORDER BY ts))[1], max(index_price), min(index_price), (array_agg(index_price ORDER BY ts DESC))[1]
		FROM (SELECT ts, index_price, (floor((extract(epoch from ts) - $4) / $2) * $2 + $4)::bigint AS b FROM fut_mark_history
		      WHERE symbol=$1 AND index_price > 0 AND ts >= now() - make_interval(secs => $2 * $3)) t
		GROUP BY b ORDER BY b`, sym, sec, n, anchor)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b klineBar
		if rows.Scan(&b.Open, &b.O, &b.H, &b.L, &b.C) == nil {
			out = append(out, b)
		}
	}
	return out
}

// loadSymbolTags maps symbol -> market_categories ids (table owned by
// market-data in the shared DB). Missing table / errors -> empty map.
func loadSymbolTags(ctx context.Context, db queryer) map[string][]string {
	out := map[string][]string{}
	rows, err := db.QueryContext(ctx, `SELECT id, array_to_string(symbols, ',') FROM market_categories ORDER BY sort, id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, syms string
		if rows.Scan(&id, &syms) != nil {
			continue
		}
		for _, sym := range strings.Split(syms, ",") {
			if sym = strings.TrimSpace(sym); sym != "" {
				out[sym] = append(out[sym], id)
			}
		}
	}
	return out
}
