package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// handlers.go: HTTP handlers for /options/*.

// hInstruments GET /options/instruments. Lists every active contract.
func (s *Server) hInstruments(w http.ResponseWriter, r *http.Request) {
	underlying := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("underlying")))
	all := s.listInstruments()
	out := make([]map[string]any, 0, len(all))
	for _, in := range all {
		if underlying != "" && in.Underlying != underlying {
			continue
		}
		if in.Status != "active" {
			continue
		}
		out = append(out, map[string]any{
			"id":            in.ID,
			"name":          in.Name,
			"underlying":    in.Underlying,
			"quote":         in.Quote,
			"type":          in.OptionType,
			"strike":        in.Strike,
			"expiry":        in.Expiry.UTC().Format(time.RFC3339),
			"contractSize":  in.ContractSize,
			"tickSize":      in.TickSize,
			"minQty":        in.MinQty,
			"status":        in.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"instruments": out})
}

// hChain GET /options/chain?underlying=BTC&expiry=2026-12-26
// Returns the two-column strike × (call|put) grid.
func (s *Server) hChain(w http.ResponseWriter, r *http.Request) {
	underlying := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("underlying")))
	expiry := strings.TrimSpace(r.URL.Query().Get("expiry"))
	if underlying == "" || expiry == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "underlying 和 expiry 必填")
		return
	}
	exp, err := parseExpiry(expiry)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "expiry 格式应为 YYYY-MM-DD 或 ISO8601")
		return
	}
	type row struct {
		Strike decimal.Decimal `json:"strike"`
		Call   map[string]any  `json:"call,omitempty"`
		Put    map[string]any  `json:"put,omitempty"`
	}
	byStrike := map[string]*row{}
	for _, in := range s.listInstruments() {
		if in.Underlying != underlying || !sameDay(in.Expiry, exp) {
			continue
		}
		if in.Status != "active" {
			continue
		}
		key := in.Strike.String()
		r := byStrike[key]
		if r == nil {
			r = &row{Strike: in.Strike}
			byStrike[key] = r
		}
		leg := map[string]any{
			"instrument": in.Name,
			"id":         in.ID,
		}
		if m, ok := s.mark(in.ID); ok {
			leg["mark"] = m.Mark
			leg["iv"] = m.IV
			leg["delta"] = m.Delta
		}
		if in.OptionType == "call" {
			r.Call = leg
		} else {
			r.Put = leg
		}
	}
	out := make([]*row, 0, len(byStrike))
	for _, r := range byStrike {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Strike.LessThan(out[j].Strike) })
	writeJSON(w, http.StatusOK, map[string]any{
		"underlying": underlying, "expiry": exp.UTC().Format(time.RFC3339),
		"chain": out,
	})
}

// hTicker GET /options/ticker/{instrument}
func (s *Server) hTicker(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("instrument")
	in, err := s.instrumentByName(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	bid, _ := s.bestBid(r.Context(), in.ID)
	ask, _ := s.bestAsk(r.Context(), in.ID)
	var last decimal.Decimal
	_ = s.db.QueryRowContext(r.Context(),
		`SELECT price FROM opt_trades WHERE instrument_id=$1 ORDER BY id DESC LIMIT 1`,
		in.ID).Scan(&last)
	m, okMark := s.mark(in.ID)
	resp := map[string]any{
		"instrument": in.Name,
		"bid":        bid, "ask": ask, "last": last,
		"underlying": m.Underlying,
	}
	if okMark {
		resp["mark"] = m.Mark
		resp["iv"] = m.IV
		resp["delta"] = m.Delta
		resp["gamma"] = m.Gamma
		resp["theta"] = m.Theta
		resp["vega"] = m.Vega
		resp["timestamp"] = m.At.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// hPlace POST /options/orders
func (s *Server) hPlace(w http.ResponseWriter, r *http.Request, uid string) {
	var req PlaceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求体不合法")
		return
	}
	out, err := s.Place(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// hCancel DELETE /options/orders/{id}
func (s *Server) hCancel(w http.ResponseWriter, r *http.Request, uid string) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	out, err := s.CancelOrder(r.Context(), uid, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// hMyOrders GET /options/orders
func (s *Server) hMyOrders(w http.ResponseWriter, r *http.Request, uid string) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	q := `SELECT o.id, i.name, o.instrument_id, o.side, o.order_type,
	             o.price, o.qty, o.filled_qty, o.avg_fill_price, o.status,
	             o.created_at, o.updated_at
	        FROM opt_orders o JOIN opt_instruments i ON i.id=o.instrument_id
	       WHERE o.user_id=$1`
	args := []any{uid}
	if status != "" {
		switch status {
		case "open":
			q += " AND o.status IN ('open','partial')"
		case "closed":
			q += " AND o.status IN ('filled','cancelled','rejected')"
		default:
			q += " AND o.status=$2"
			args = append(args, status)
		}
	}
	q += " ORDER BY o.id DESC LIMIT 200"
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []OrderRow{}
	for rows.Next() {
		var o OrderRow
		var price, avg decimal.NullDecimal
		if err := rows.Scan(&o.ID, &o.Instrument, &o.InstrumentID, &o.Side, &o.Type,
			&price, &o.Qty, &o.FilledQty, &avg, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			writeErr(w, err)
			return
		}
		if price.Valid {
			o.Price = price.Decimal
		}
		if avg.Valid {
			o.AvgFillPrice = avg.Decimal
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

// hPositions GET /options/positions
func (s *Server) hPositions(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `
	    SELECT p.instrument_id, i.name, i.option_type, i.strike, i.expiry,
	           p.qty, p.avg_price, p.realized_pnl, p.margin, p.settled
	      FROM opt_positions p JOIN opt_instruments i ON i.id=p.instrument_id
	     WHERE p.user_id=$1 AND (p.qty <> 0 OR NOT p.settled)
	     ORDER BY i.expiry ASC, i.strike ASC`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	type pos struct {
		InstrumentID int64           `json:"instrumentId"`
		Instrument   string          `json:"instrument"`
		Type         string          `json:"type"`
		Strike       decimal.Decimal `json:"strike"`
		Expiry       time.Time       `json:"expiry"`
		Qty          decimal.Decimal `json:"qty"`
		AvgPrice     decimal.Decimal `json:"avgPrice"`
		Mark         decimal.Decimal `json:"mark"`
		UnrealizedPnl decimal.Decimal `json:"unrealizedPnl"`
		RealizedPnl  decimal.Decimal `json:"realizedPnl"`
		Margin       decimal.Decimal `json:"margin"`
		Settled      bool            `json:"settled"`
	}
	out := []pos{}
	for rows.Next() {
		var p pos
		if err := rows.Scan(&p.InstrumentID, &p.Instrument, &p.Type, &p.Strike, &p.Expiry,
			&p.Qty, &p.AvgPrice, &p.RealizedPnl, &p.Margin, &p.Settled); err != nil {
			writeErr(w, err)
			return
		}
		if m, ok := s.mark(p.InstrumentID); ok {
			p.Mark = m.Mark
			p.UnrealizedPnl = m.Mark.Sub(p.AvgPrice).Mul(p.Qty)
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": out})
}

// hTrades GET /options/trades — user fill history.
func (s *Server) hTrades(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `
	    SELECT t.id, i.name, t.taker_side, t.price, t.qty, t.taker_fee,
	           t.created_at,
	           CASE WHEN t.taker_user_id=$1 THEN 'taker' ELSE 'maker' END AS role,
	           CASE WHEN t.taker_user_id=$1 THEN t.taker_side
	                ELSE CASE WHEN t.taker_side='buy' THEN 'sell' ELSE 'buy' END
	           END AS my_side
	      FROM opt_trades t JOIN opt_instruments i ON i.id=t.instrument_id
	     WHERE t.taker_user_id=$1 OR t.maker_user_id=$1
	     ORDER BY t.id DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	type trade struct {
		ID         int64           `json:"id"`
		Instrument string          `json:"instrument"`
		Side       string          `json:"side"`    // my side
		Role       string          `json:"role"`    // taker | maker
		Price      decimal.Decimal `json:"price"`
		Qty        decimal.Decimal `json:"qty"`
		Fee        decimal.Decimal `json:"fee"`
		Time       time.Time       `json:"time"`
	}
	out := []trade{}
	for rows.Next() {
		var t trade
		var takerSide string
		if err := rows.Scan(&t.ID, &t.Instrument, &takerSide, &t.Price, &t.Qty, &t.Fee,
			&t.Time, &t.Role, &t.Side); err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": out})
}

// parseExpiry accepts YYYY-MM-DD, YYYY-MM-DDTHH:MM:SSZ or ISO8601 with offset.
func parseExpiry(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errNotFound
}

func sameDay(a, b time.Time) bool {
	a, b = a.UTC(), b.UTC()
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
