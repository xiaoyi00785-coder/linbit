package main

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// DCA routes are mounted alongside the grid routes in Server.Routes.
func (s *Server) dcaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /strategy/dca/plans", s.user(s.hDCACreate))
	mux.HandleFunc("GET /strategy/dca/plans", s.user(s.hDCAList))
	mux.HandleFunc("GET /strategy/dca/plans/{id}", s.user(s.hDCAGet))
	mux.HandleFunc("POST /strategy/dca/plans/{id}/pause", s.user(s.hDCAPause))
	mux.HandleFunc("POST /strategy/dca/plans/{id}/resume", s.user(s.hDCAResume))
	mux.HandleFunc("DELETE /strategy/dca/plans/{id}", s.user(s.hDCADelete))
	mux.HandleFunc("GET /strategy/dca/plans/{id}/orders", s.user(s.hDCAOrders))
	mux.HandleFunc("GET /strategy/dca/summary", s.user(s.hDCASummary))
	if s.cfg.DevMode {
		mux.HandleFunc("POST /internal/dev/dca/tick", func(w http.ResponseWriter, r *http.Request) {
			s.TickDCA(r.Context())
			common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		})
	}
}

func (s *Server) hDCACreate(w http.ResponseWriter, r *http.Request, uid string) {
	var req dcaCreateReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求体不合法")
		return
	}
	p, err := s.CreateDCA(r.Context(), uid, req)
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, dcaPlanView(p))
}

func (s *Server) hDCAList(w http.ResponseWriter, r *http.Request, uid string) {
	status := r.URL.Query().Get("status")
	where, args := `WHERE user_id=$1 AND status<>'stopped' ORDER BY id DESC LIMIT 200`, []any{uid}
	switch status {
	case "stopped":
		where = `WHERE user_id=$1 AND status='stopped' ORDER BY id DESC LIMIT 200`
	case "all":
		where = `WHERE user_id=$1 ORDER BY id DESC LIMIT 200`
	case "active", "paused":
		where = `WHERE user_id=$1 AND status=$2 ORDER BY id DESC LIMIT 200`
		args = append(args, status)
	}
	ps, err := s.dcaPlans(r.Context(), where, args...)
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	items := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		items = append(items, dcaPlanView(p))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) hDCAGet(w http.ResponseWriter, r *http.Request, uid string) {
	p, err := s.dcaByPath(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, dcaPlanView(p))
}

func (s *Server) hDCAPause(w http.ResponseWriter, r *http.Request, uid string) {
	p, err := s.dcaByPath(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	p, err = s.PauseDCA(r.Context(), uid, p.ID)
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, dcaPlanView(p))
}

func (s *Server) hDCAResume(w http.ResponseWriter, r *http.Request, uid string) {
	p, err := s.dcaByPath(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	p, err = s.ResumeDCA(r.Context(), uid, p.ID)
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, dcaPlanView(p))
}

func (s *Server) hDCADelete(w http.ResponseWriter, r *http.Request, uid string) {
	p, err := s.dcaByPath(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	if err := s.DeleteDCA(r.Context(), uid, p.ID); err != nil {
		writeDCAErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": strconv.FormatInt(p.ID, 10)})
}

func (s *Server) hDCAOrders(w http.ResponseWriter, r *http.Request, uid string) {
	p, err := s.dcaByPath(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,order_id,side,status,requested_quote,requested_base,
		filled_base,filled_quote,fee_base,fee_quote,avg_price,error,created_at,done_at
		FROM dca_executions WHERE plan_id=$1 ORDER BY id DESC LIMIT 500`, p.ID)
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var oid, side, status string
		var reqQ, reqB, fb, fq, feeB, feeQ decimal.Decimal
		var avg decimal.NullDecimal
		var errStr sql.NullString
		var createdAt time.Time
		var done sql.NullTime
		if err := rows.Scan(&id, &oid, &side, &status, &reqQ, &reqB, &fb, &fq, &feeB, &feeQ, &avg, &errStr, &createdAt, &done); err != nil {
			writeDCAErr(w, err)
			return
		}
		m := map[string]any{
			"id":              strconv.FormatInt(id, 10),
			"orderId":         oid,
			"side":            side,
			"status":          status,
			"requestedQuote":  reqQ.String(),
			"requestedBase":   reqB.String(),
			"filledBase":      fb.String(),
			"filledQuote":     fq.String(),
			"feeBase":         feeB.String(),
			"feeQuote":        feeQ.String(),
			"avgPrice":        nd(avg),
			"error":           nullStr(errStr),
			"createdAt":       createdAt.UTC().Format(time.RFC3339),
		}
		if done.Valid {
			m["doneAt"] = done.Time.UTC().Format(time.RFC3339)
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// hDCASummary aggregates the user's active and lifetime DCA stats.
func (s *Server) hDCASummary(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	// Plan counts.
	var active, paused, stopped int
	if err := s.db.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE status='active'),
		count(*) FILTER (WHERE status='paused'),
		count(*) FILTER (WHERE status='stopped')
		FROM dca_plans WHERE user_id=$1`, uid).Scan(&active, &paused, &stopped); err != nil {
		writeDCAErr(w, err)
		return
	}
	// Per-symbol/side aggregation across all executions.
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, side,
		sum(filled_base), sum(filled_quote), sum(fee_base), sum(fee_quote), count(*)
		FROM dca_executions WHERE user_id=$1 AND status='filled'
		GROUP BY symbol, side ORDER BY symbol, side`, uid)
	if err != nil {
		writeDCAErr(w, err)
		return
	}
	defer rows.Close()
	type sym struct {
		Symbol   string          `json:"symbol"`
		Side     string          `json:"side"`
		Base     decimal.Decimal `json:"base"`
		Quote    decimal.Decimal `json:"quote"`
		FeeBase  decimal.Decimal `json:"feeBase"`
		FeeQuote decimal.Decimal `json:"feeQuote"`
		Avg      string          `json:"avgPrice"`
		Runs     int             `json:"runs"`
		PnL      string          `json:"unrealizedPnl,omitempty"`
	}
	items := []sym{}
	for rows.Next() {
		var s sym
		if err := rows.Scan(&s.Symbol, &s.Side, &s.Base, &s.Quote, &s.FeeBase, &s.FeeQuote, &s.Runs); err != nil {
			writeDCAErr(w, err)
			return
		}
		if s.Base.Sign() > 0 {
			s.Avg = s.Quote.Div(s.Base).Round(8).String()
		}
		items = append(items, s)
	}
	// Mark unrealized PnL for buys at last price when market data is reachable.
	for i := range items {
		if items[i].Side != "buy" || items[i].Base.Sign() <= 0 {
			continue
		}
		p, err := s.priceCall(ctx, items[i].Symbol)
		if err != nil {
			continue
		}
		pnl := items[i].Base.Mul(p).Sub(items[i].Quote).Round(8)
		items[i].PnL = pnl.String()
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"plans":   map[string]int{"active": active, "paused": paused, "stopped": stopped},
		"symbols": items,
	})
}

// ---- helpers ----

func (s *Server) dcaByPath(ctx context.Context, uid, raw string) (DCAPlan, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return DCAPlan{}, bad("PLAN_NOT_FOUND", "定投计划不存在")
	}
	return s.ownedDCA(ctx, uid, id)
}

func dcaPlanView(p DCAPlan) map[string]any {
	m := map[string]any{
		"id":              strconv.FormatInt(p.ID, 10),
		"pair":            p.Symbol,
		"symbol":          p.Symbol,
		"baseAsset":       p.Base,
		"quoteAsset":      p.Quote,
		"side":            p.Side,
		"baseAmount":      p.AmountPerRun.String(),
		"amountPerRun":    p.AmountPerRun.String(),
		"intervalSeconds": p.IntervalSec,
		"triggerType":     p.TriggerType,
		"triggerPrice":    nd(p.TriggerPrice),
		"startAt":         p.StartAt.UTC().Format(time.RFC3339),
		"endAt":           nt(p.EndAt),
		"nextRunAt":       p.NextRunAt.UTC().Format(time.RFC3339),
		"lastRunAt":       nt(p.LastRunAt),
		"status":          p.Status,
		"stopReason":      ns(p.StopReason),
		"totalRuns":       p.TotalRuns,
		"totalBase":       p.TotalBase.String(),
		"totalQuote":      p.TotalQuote.String(),
		"totalFeeBase":    p.TotalFeeBase.String(),
		"totalFeeQuote":   p.TotalFeeQuote.String(),
		"createdAt":       p.CreatedAt.UTC().Format(time.RFC3339),
		"stoppedAt":       nt(p.StoppedAt),
	}
	if p.TotalBase.Sign() > 0 {
		m["averagePrice"] = p.TotalQuote.Div(p.TotalBase).Round(8).String()
	}
	return m
}

func nt(t sql.NullTime) any {
	if !t.Valid {
		return nil
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func ns(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}

func nullStr(s sql.NullString) any { return ns(s) }

func writeDCAErr(w http.ResponseWriter, err error) { writeErr(w, err) }
