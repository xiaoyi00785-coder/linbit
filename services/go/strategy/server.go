package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	// Public.
	mux.HandleFunc("GET /strategy/config", s.hConfig)
	mux.HandleFunc("GET /strategy/grid/square", s.hSquare)
	mux.HandleFunc("POST /strategy/grid/square/{id}/copy", s.user(s.hCopy))
	// User (X-User-Id injected by the gateway).
	mux.HandleFunc("GET /strategy/grid/preview", s.user(s.hPreview))
	mux.HandleFunc("POST /strategy/grid", s.user(s.hCreate))
	mux.HandleFunc("GET /strategy/grid", s.user(s.hList))
	mux.HandleFunc("GET /strategy/grid/{id}", s.user(s.hGet))
	mux.HandleFunc("GET /strategy/grid/{id}/orders", s.user(s.hOrders))
	mux.HandleFunc("POST /strategy/grid/{id}/stop", s.user(s.hStop))
	if s.cfg.DevMode {
		mux.HandleFunc("POST /internal/dev/grid/tick", func(w http.ResponseWriter, r *http.Request) {
			s.Tick(r.Context())
			common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		})
	}
	return mux
}

func (s *Server) user(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-Id")
		if uid == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		next(w, r, uid)
	}
}

func writeErr(w http.ResponseWriter, err error) {
	var ue *userErr
	if errors.As(err, &ue) {
		st := http.StatusBadRequest
		if ue.Code == "BOT_NOT_FOUND" {
			st = http.StatusNotFound
		}
		common.WriteError(w, st, ue.Code, ue.Msg)
		return
	}
	log.Printf("strategy: %v", err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func (s *Server) hConfig(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"modes": []string{"arithmetic", "geometric"}, "minGrids": s.cfg.MinGrids, "maxGrids": s.cfg.MaxGrids,
		"minInvestment": s.cfg.MinInvestment.String(), "maxBotsPerUser": s.cfg.MaxBotsPerUser,
		"feeRate": s.cfg.FeeRate.String(), "sellBaseOnStop": s.cfg.DefaultSellBase,
	})
}

func (s *Server) hPreview(w http.ResponseWriter, r *http.Request, uid string) {
	q := r.URL.Query()
	n, _ := strconv.Atoi(q.Get("gridCount"))
	req := createReq{Symbol: q.Get("symbol"), Mode: q.Get("mode"), Lower: q.Get("lowerPrice"), Upper: q.Get("upperPrice"),
		Grids: n, Investment: q.Get("investment")}
	if sl := q.Get("stopLossPrice"); sl != "" {
		req.StopLoss = &sl
	}
	if tp := q.Get("takeProfitPrice"); tp != "" {
		req.TakeProfit = &tp
	}
	v, err := s.validate(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	lo, hi := GridProfitRange(v.levels, s.cfg.FeeRate)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"symbol": v.sym.Symbol, "mode": v.mode, "currentPrice": v.price.String(), "levels": decStrs(v.levels),
		"qtyPerGrid": v.plan.Qty.String(), "buyOrders": len(v.plan.BuyLevels), "sellOrders": len(v.plan.SellLevels),
		"initialBuyQuote":  v.plan.InitQuote.String(),
		"profitPerGridMin": lo.Round(6).String(), "profitPerGridMax": hi.Round(6).String(),
	})
}

func decStrs(ds []decimal.Decimal) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.String()
	}
	return out
}

func (s *Server) hCreate(w http.ResponseWriter, r *http.Request, uid string) {
	var req createReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求体不合法")
		return
	}
	b, err := s.Create(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, s.view(r.Context(), b, true))
}

func (s *Server) owned(ctx context.Context, uid, raw string) (Bot, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return Bot{}, bad("BOT_NOT_FOUND", "网格不存在")
	}
	b, err := s.bot(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && b.UserID != uid) {
		return Bot{}, bad("BOT_NOT_FOUND", "网格不存在")
	}
	return b, err
}

func (s *Server) hList(w http.ResponseWriter, r *http.Request, uid string) {
	where, args := `WHERE user_id=$1 AND status<>'stopped' ORDER BY id DESC`, []any{uid}
	switch r.URL.Query().Get("status") {
	case "stopped":
		where = `WHERE user_id=$1 AND status='stopped' ORDER BY id DESC LIMIT 100`
	case "all":
		where = `WHERE user_id=$1 ORDER BY id DESC LIMIT 100`
	}
	bs, err := s.bots(r.Context(), where, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := []map[string]any{}
	for _, b := range bs {
		items = append(items, s.view(r.Context(), b, false))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) hGet(w http.ResponseWriter, r *http.Request, uid string) {
	b, err := s.owned(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.view(r.Context(), b, true))
}

func (s *Server) hOrders(w http.ResponseWriter, r *http.Request, uid string) {
	b, err := s.owned(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	where := ""
	if st := r.URL.Query().Get("status"); st == "open" || st == "filled" || st == "canceled" {
		where = " AND status='" + st + "'"
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT level,side,kind,price,quantity,order_id,status,filled_qty,filled_quote,
		fee_base,fee_quote,profit,created_at,done_at FROM grid_orders WHERE bot_id=$1`+where+` ORDER BY id DESC LIMIT 500`, b.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var level int
		var side, kind, oid, st string
		var price, profit decimal.NullDecimal
		var qty, fq, fquote, fb, fqf decimal.Decimal
		var at time.Time
		var done sql.NullTime
		if err := rows.Scan(&level, &side, &kind, &price, &qty, &oid, &st, &fq, &fquote, &fb, &fqf, &profit, &at, &done); err != nil {
			writeErr(w, err)
			return
		}
		m := map[string]any{"level": level, "side": side, "kind": kind, "quantity": qty.String(), "orderId": oid,
			"status": st, "filledQuantity": fq.String(), "filledQuote": fquote.String(), "baseFee": fb.String(),
			"quoteFee": fqf.String(), "createdAt": at.UTC().Format(time.RFC3339)}
		m["price"] = nd(price)
		m["profit"] = nd(profit)
		if done.Valid {
			m["doneAt"] = done.Time.UTC().Format(time.RFC3339)
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func nd(d decimal.NullDecimal) any {
	if !d.Valid {
		return nil
	}
	return d.Decimal.String()
}

func (s *Server) hStop(w http.ResponseWriter, r *http.Request, uid string) {
	var body struct {
		SellBase *bool `json:"sellBase"`
	}
	if r.ContentLength > 0 {
		if err := common.DecodeJSON(r, &body); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求体不合法")
			return
		}
	}
	b, err := s.owned(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err = s.Stop(r.Context(), uid, b.ID, body.SellBase)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.view(r.Context(), b, true))
}

// ---- stats view ----

type stats struct {
	price, value, total, rate, unreal, apr decimal.Decimal
	runtime                                time.Duration
	dd7                                    decimal.Decimal
}

func (s *Server) stats(ctx context.Context, b Bot) stats {
	var st stats
	end := time.Now()
	if b.Status == "stopped" && b.FinalPrice.Valid {
		st.price = b.FinalPrice.Decimal
		if b.StoppedAt.Valid {
			end = b.StoppedAt.Time
		}
	} else if p, err := s.lastPrice(ctx, b.Symbol); err == nil {
		st.price = p
	} else {
		st.price = b.StartPrice
	}
	st.value = b.Value(st.price)
	if b.Status == "stopped" && b.FinalValue.Valid {
		st.value = b.FinalValue.Decimal
	}
	st.total = st.value.Sub(b.Investment)
	st.rate = st.total.Div(b.Investment)
	st.unreal = st.total.Sub(b.GridProfit)
	st.runtime = end.Sub(b.CreatedAt)
	if days := st.runtime.Hours() / 24; days > 0 {
		st.apr = b.GridProfit.Div(b.Investment).Mul(decimal.NewFromFloat(365 / days))
	}
	st.dd7 = s.drawdownSince(ctx, b.ID, end.Add(-7*24*time.Hour))
	return st
}

func (s *Server) drawdownSince(ctx context.Context, id int64, since time.Time) decimal.Decimal {
	rows, err := s.db.QueryContext(ctx, `SELECT value FROM grid_snapshots WHERE bot_id=$1 AND at>=$2 ORDER BY at`, id, since)
	if err != nil {
		return decimal.Zero
	}
	defer rows.Close()
	var peak, dd decimal.Decimal
	for rows.Next() {
		var v decimal.Decimal
		if rows.Scan(&v) != nil {
			break
		}
		if v.GreaterThan(peak) {
			peak = v
		}
		if peak.Sign() > 0 {
			if d := peak.Sub(v).Div(peak); d.GreaterThan(dd) {
				dd = d
			}
		}
	}
	return dd.Round(8)
}

func (s *Server) view(ctx context.Context, b Bot, detail bool) map[string]any {
	st := s.stats(ctx, b)
	m := map[string]any{
		"id": strconv.FormatInt(b.ID, 10), "symbol": b.Symbol, "mode": b.Mode, "lowerPrice": b.Lower.String(),
		"upperPrice": b.Upper.String(), "gridCount": b.Grids, "investment": b.Investment.String(),
		"qtyPerGrid": b.Qty.String(), "stopLossPrice": nd(b.StopLoss), "takeProfitPrice": nd(b.TakeProfit),
		"sellBaseOnStop": b.SellOnStop, "startPrice": b.StartPrice.String(), "status": b.Status,
		"stopReason": nil, "createdAt": b.CreatedAt.UTC().Format(time.RFC3339),
		"currentPrice": st.price.String(), "baseHeld": b.BaseQty.String(), "quoteBalance": b.QuoteBal.Round(8).String(),
		"value": st.value.Round(8).String(), "gridProfit": b.GridProfit.Round(8).String(),
		"unrealizedPnl": st.unreal.Round(8).String(), "totalProfit": st.total.Round(8).String(),
		"profitRate": st.rate.Round(6).String(), "gridApr": st.apr.Round(6).String(),
		"cycles": b.Cycles, "runtimeSeconds": int64(st.runtime.Seconds()), "maxDrawdown": b.MaxDD.String(),
		"maxDrawdown7d": st.dd7.String(), "feesQuote": b.FeesQuote.Round(8).String(), "feesBase": b.FeesBase.String(),
		"baseAsset": b.Base, "quoteAsset": b.Quote,
	}
	if b.StopReason.Valid {
		m["stopReason"] = b.StopReason.String
	}
	if b.StoppedAt.Valid {
		m["stoppedAt"] = b.StoppedAt.Time.UTC().Format(time.RFC3339)
	}
	if detail {
		var open int
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM grid_orders WHERE bot_id=$1 AND status='open'`, b.ID).Scan(&open)
		m["openOrders"] = open
		if sym, err := s.symbol(ctx, b.Symbol); err == nil {
			if lv, err := s.levelsOf(b, sym); err == nil {
				m["levels"] = decStrs(lv)
			}
		}
	}
	return m
}

// hSquare is the public 策略广场: running bots, anonymised, sortable by
// default (profit rate), runtime or roi.
func (s *Server) hSquare(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	bs, err := s.bots(r.Context(), `WHERE status='running' AND created_at <= now() - $1::interval ORDER BY id DESC LIMIT 500`,
		strconv.FormatInt(int64(s.cfg.SquareMinRun.Seconds()), 10)+" seconds")
	if err != nil {
		writeErr(w, err)
		return
	}
	prices := map[string]decimal.Decimal{}
	type row struct {
		m       map[string]any
		rate    decimal.Decimal
		runtime time.Duration
		copies  int
	}
	rows := []row{}
	for _, b := range bs {
		if _, ok := prices[b.Symbol]; !ok {
			p, err := s.lastPrice(r.Context(), b.Symbol)
			if err != nil {
				p = b.StartPrice
			}
			prices[b.Symbol] = p
		}
		p := prices[b.Symbol]
		rate := b.Value(p).Sub(b.Investment).Div(b.Investment)
		rt := time.Since(b.CreatedAt)
		rows = append(rows, row{m: map[string]any{
			"id": strconv.FormatInt(b.ID, 10), "symbol": b.Symbol, "mode": b.Mode, "lowerPrice": b.Lower.String(),
			"upperPrice": b.Upper.String(), "gridCount": b.Grids, "profitRate": rate.Round(6).String(),
			"runtimeSeconds": int64(rt.Seconds()), "maxDrawdown7d": s.drawdownSince(r.Context(), b.ID, time.Now().Add(-7*24*time.Hour)).String(),
			"cycles": b.Cycles,
		}, rate: rate, runtime: rt})
	}
	ids := make([]int64, 0, len(bs))
	for _, b := range bs {
		ids = append(ids, b.ID)
	}
	counts := s.copyCounts(r.Context(), ids)
	for i, b := range bs {
		rows[i].m["copyCount"] = counts[b.ID]
		rows[i].copies = counts[b.ID]
	}
	switch q.Get("sort") {
	case "copies":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].copies > rows[j].copies })
	case "runtime":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].runtime > rows[j].runtime })
	default: // "default" and "roi"
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].rate.GreaterThan(rows[j].rate) })
	}
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 9
	}
	items := []map[string]any{}
	for i := (page - 1) * size; i < len(rows) && i < page*size; i++ {
		items = append(items, rows[i].m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(rows), "page": page, "pageSize": size})
}
