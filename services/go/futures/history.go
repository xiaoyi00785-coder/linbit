package main

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// history.go: closed-position history (Binance "Position History" / OKX "Positions history").

func (p *Position) resetLifecycle(now time.Time) {
	p.OpenedAt = sql.NullTime{}
	p.OpenQty, p.OpenQuote, p.CloseQty, p.CloseQuote = dZero, dZero, dZero, dZero
	p.MaxQty, p.Fees, p.Funding = dZero, dZero, dZero
}

// recordHistory writes the position that just ended (oldQ was its signed size).
// The close status comes from s.histStatus (set by liquidation / ADL under s.mu).
func (s *Server) recordHistory(ctx context.Context, tx *sql.Tx, p Position, oldQ decimal.Decimal) error {
	if strings.HasPrefix(p.UserID, "system:") {
		return nil
	}
	dir := "long"
	if oldQ.Sign() < 0 {
		dir = "short"
	}
	entry, closePx := p.Entry, dZero
	if p.OpenQty.Sign() > 0 {
		entry = p.OpenQuote.Div(p.OpenQty)
	}
	if p.CloseQty.Sign() > 0 {
		closePx = p.CloseQuote.Div(p.CloseQty)
	}
	opened := p.UpdatedAt
	if p.OpenedAt.Valid {
		opened = p.OpenedAt.Time
	}
	if opened.IsZero() {
		opened = s.now()
	}
	status := s.histStatus
	if status == "" {
		status = "closed"
	}
	maxQ := dmax(p.MaxQty, oldQ.Abs())
	net := money(p.Realized.Sub(p.Fees).Add(p.Funding))
	_, err := tx.ExecContext(ctx, `INSERT INTO fut_position_history (user_id, symbol, position_side, direction, margin_mode, leverage, entry_price, close_price,
		max_qty, closed_qty, realized_pnl, fees, funding, net_pnl, status, opened_at, closed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		p.UserID, p.Symbol, normSide(p.Side), dir, p.Mode, p.Leverage, entry.Round(8), closePx.Round(8), maxQ, p.CloseQty,
		money(p.Realized), money(p.Fees), money(p.Funding), net, status, opened, s.now())
	return err
}

type PosHistory struct {
	ID        int64     `json:"id"`
	Symbol    string    `json:"symbol"`
	PosSide   string    `json:"positionSide"`
	Direction string    `json:"side"`
	Mode      string    `json:"marginMode"`
	Leverage  int       `json:"leverage"`
	Entry     string    `json:"entryPrice"`
	Close     string    `json:"closePrice"`
	MaxQty    string    `json:"maxQty"`
	ClosedQty string    `json:"closedQty"`
	Realized  string    `json:"realizedPnl"`
	Fees      string    `json:"fee"`
	Funding   string    `json:"fundingFee"`
	Net       string    `json:"netPnl"`
	Status    string    `json:"status"`
	OpenedAt  time.Time `json:"openTime"`
	ClosedAt  time.Time `json:"closeTime"`
}

// GET /futures/positions/history?symbol=&startTime=&endTime=&cursor=&limit=
// Newest first; nextCursor is the id to pass as cursor for the next page.
func (s *Server) hPositionHistory(w http.ResponseWriter, r *http.Request, uid string) {
	q := r.URL.Query()
	qs := `SELECT id, symbol, position_side, direction, margin_mode, leverage, entry_price, close_price, max_qty, closed_qty, realized_pnl, fees, funding, net_pnl, status, opened_at, closed_at
		FROM fut_position_history WHERE user_id=$1`
	args := []any{uid}
	add := func(cond string, v any) {
		args = append(args, v)
		qs += ` AND ` + strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args)))
	}
	if sym := q.Get("symbol"); sym != "" {
		add("symbol=?", strings.ToUpper(sym))
	}
	if v, err := strconv.ParseInt(q.Get("startTime"), 10, 64); err == nil && v > 0 {
		add("closed_at>=?", time.UnixMilli(v))
	}
	if v, err := strconv.ParseInt(q.Get("endTime"), 10, 64); err == nil && v > 0 {
		add("closed_at<=?", time.UnixMilli(v))
	}
	if v, err := strconv.ParseInt(q.Get("cursor"), 10, 64); err == nil && v > 0 {
		add("id<?", v)
	}
	lim := limitParam(r, 50, 200)
	args = append(args, lim+1)
	qs += ` ORDER BY id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := s.db.QueryContext(r.Context(), qs, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []PosHistory{}
	for rows.Next() {
		var h PosHistory
		var e, c, mq, cq, rp, f, fu, n decimal.Decimal
		if err := rows.Scan(&h.ID, &h.Symbol, &h.PosSide, &h.Direction, &h.Mode, &h.Leverage, &e, &c, &mq, &cq, &rp, &f, &fu, &n, &h.Status, &h.OpenedAt, &h.ClosedAt); err != nil {
			writeErr(w, err)
			return
		}
		h.Entry, h.Close, h.MaxQty, h.ClosedQty = e.String(), c.String(), mq.String(), cq.String()
		h.Realized, h.Fees, h.Funding, h.Net = money(rp).String(), money(f).String(), money(fu).String(), money(n).String()
		out = append(out, h)
	}
	var next any
	if len(out) > lim {
		out = out[:lim]
		next = strconv.FormatInt(out[lim-1].ID, 10)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"positions": out, "nextCursor": next})
}
