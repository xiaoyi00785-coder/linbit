package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// marketdata.go: public futures market data.

func (s *Server) hContracts(w http.ResponseWriter, r *http.Request) {
	out := make([]Contract, 0, len(s.symbols))
	for _, sym := range s.symbols {
		out = append(out, s.contracts[sym])
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
	change := dZero
	if open.Valid && open.Decimal.Sign() > 0 && ps.Last.Sign() > 0 {
		change = ps.Last.Sub(open.Decimal).Div(open.Decimal).Round(6)
	}
	z := func(n decimal.NullDecimal) decimal.Decimal {
		if n.Valid {
			return n.Decimal
		}
		return dZero
	}
	return map[string]any{
		"symbol": sym, "lastPrice": ps.Last, "markPrice": ps.Mark, "indexPrice": ps.Index,
		"fundingRate": s.predictedRate(ps), "lastFundingRate": ps.LastRate,
		"nextFundingTime": s.nextFunding(s.now()).UnixMilli(),
		"openPrice24h":    z(open), "highPrice24h": z(high), "lowPrice24h": z(low),
		"volume24h": z(vol), "turnover24h": money(z(turn)), "priceChangePercent24h": change,
		"openInterest": oi, "priceAvailable": ps.Mark.Sign() > 0,
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

var klineSec = map[string]int64{"1m": 60, "5m": 300, "15m": 900, "30m": 1800, "1h": 3600, "4h": 14400, "1d": 86400}

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
	sec, ok := klineSec[iv]
	if !ok {
		writeErr(w, bad("VALIDATION_ERROR", "interval 仅支持 1m/5m/15m/30m/1h/4h/1d"))
		return
	}
	n := limitParam(r, 200, 1000)
	rows, err := s.db.QueryContext(r.Context(), `SELECT b, (array_agg(price ORDER BY id))[1], max(price), min(price), (array_agg(price ORDER BY id DESC))[1], sum(qty), sum(qty*price)
		FROM (SELECT id, price, qty, (floor(extract(epoch from created_at) / $2) * $2)::bigint AS b FROM fut_trades
		      WHERE symbol=$1 AND created_at >= now() - make_interval(secs => $2 * $3)) t
		GROUP BY b ORDER BY b`, c.Symbol, sec, n)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var b int64
		var o, h, l, cl, v, t decimal.Decimal
		if rows.Scan(&b, &o, &h, &l, &cl, &v, &t) == nil {
			out = append(out, map[string]any{"openTime": b * 1000, "open": o, "high": h, "low": l, "close": cl, "volume": v, "turnover": money(t)})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "interval": iv, "klines": out})
}

func (s *Server) hFundingHistory(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT funding_time, rate, mark_price FROM fut_funding_history WHERE symbol=$1 ORDER BY funding_time DESC LIMIT $2`, c.Symbol, limitParam(r, 100, 1000))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var at time.Time
		var rate, mark decimal.Decimal
		if rows.Scan(&at, &rate, &mark) == nil {
			out = append(out, map[string]any{"fundingTime": at.UnixMilli(), "fundingRate": rate, "markPrice": mark})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "history": out})
}

func (s *Server) hMarkHistory(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT ts, index_price, mark_price FROM fut_mark_history WHERE symbol=$1 ORDER BY ts DESC LIMIT $2`, c.Symbol, limitParam(r, 200, 2000))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var at time.Time
		var ip, mp decimal.Decimal
		if rows.Scan(&at, &ip, &mp) == nil {
			out = append(out, map[string]any{"time": at.UnixMilli(), "indexPrice": ip, "markPrice": mp})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "history": out})
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
