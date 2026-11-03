package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Routes:
//
//	public   /futures/market/*            (gateway: no auth)
//	private  /futures/*                   (gateway injects X-User-Id)
//	internal /internal/dev/*              (X-Internal-Token + DEV_MODE)
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// market data
	mux.HandleFunc("GET /futures/market/contracts", s.hContracts)
	mux.HandleFunc("GET /futures/market/ticker", s.hTicker)
	mux.HandleFunc("GET /futures/market/depth", s.hDepth)
	mux.HandleFunc("GET /futures/market/trades", s.hTrades)
	mux.HandleFunc("GET /futures/market/klines", s.hKlines)
	mux.HandleFunc("GET /futures/market/funding-history", s.hFundingHistory)
	mux.HandleFunc("GET /futures/market/mark-history", s.hMarkHistory)
	mux.HandleFunc("GET /futures/market/insurance-fund", s.hInsurance)
	// account
	mux.HandleFunc("GET /futures/account", s.user(s.hAccount))
	mux.HandleFunc("POST /futures/transfer", s.user(s.hTransfer))
	mux.HandleFunc("GET /futures/transfers", s.user(s.hTransfers))
	mux.HandleFunc("GET /futures/leverage", s.user(s.hGetLeverage))
	mux.HandleFunc("POST /futures/leverage", s.user(s.hSetLeverage))
	mux.HandleFunc("POST /futures/orders", s.user(s.hPlace))
	mux.HandleFunc("GET /futures/orders", s.user(s.hOrders))
	mux.HandleFunc("DELETE /futures/orders/{id}", s.user(s.hCancel))
	mux.HandleFunc("GET /futures/positions", s.user(s.hPositions))
	mux.HandleFunc("POST /futures/positions/{symbol}/tpsl", s.user(s.hTPSL))
	mux.HandleFunc("POST /futures/positions/{symbol}/margin", s.user(s.hMargin))
	mux.HandleFunc("POST /futures/positions/{symbol}/close", s.user(s.hClose))
	mux.HandleFunc("GET /futures/bills", s.user(s.hBills))
	mux.HandleFunc("GET /futures/trades", s.user(s.hMyTrades))
	// dev
	mux.HandleFunc("POST /internal/dev/index", s.dev(s.hDevIndex))
	mux.HandleFunc("POST /internal/dev/tick", s.dev(s.hDevTick))
	mux.HandleFunc("POST /internal/dev/funding", s.dev(s.hDevFunding))
	mux.HandleFunc("GET /internal/dev/audit", s.dev(s.hDevAudit))
	return mux
}

type userHandler func(w http.ResponseWriter, r *http.Request, uid string)

func (s *Server) user(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-Id")
		if uid == "" || strings.HasPrefix(uid, "system:") {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		h(w, r, uid)
	}
}

func (s *Server) dev(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.DevMode {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found")
			return
		}
		if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

func writeErr(w http.ResponseWriter, err error) {
	if a, ok := asAPI(err); ok {
		common.WriteError(w, a.Status, a.Code, a.Message)
		return
	}
	log.Printf("futures: internal error: %v", err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := common.DecodeJSON(r, v); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return false
	}
	return true
}

func limitParam(r *http.Request, def, maxV int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, maxV)
}

// ---- account handlers ----

func (s *Server) hAccount(w http.ResponseWriter, r *http.Request, uid string) {
	st, err := s.state(r.Context(), s.db, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"asset": "USDT", "walletBalance": money(st.Balance), "orderMargin": money(st.Frozen),
		"crossPositionMargin": money(st.CrossIM), "crossMaintenanceMargin": money(st.CrossMM),
		"crossUnrealizedPnl": money(st.CrossUPnL), "isolatedMargin": money(st.IsoMargin),
		"isolatedUnrealizedPnl": money(st.IsoUPnL), "availableBalance": money(dmax(st.Available, dZero)),
		"marginBalance": money(st.Equity), "positions": st.Positions,
	})
}

func (s *Server) hPositions(w http.ResponseWriter, r *http.Request, uid string) {
	st, err := s.state(r.Context(), s.db, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"positions": st.Positions})
}

func (s *Server) hTransfer(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Direction string `json:"direction"`
		Amount    string `json:"amount"`
	}
	if !decode(w, r, &req) {
		return
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		writeErr(w, bad("INVALID_AMOUNT", "划转金额不合法"))
		return
	}
	ref, status, err := s.Transfer(r.Context(), uid, strings.ToLower(req.Direction), amt)
	if err != nil {
		writeErr(w, err)
		return
	}
	if status == "failed" {
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "现货可用余额不足")
		return
	}
	code := http.StatusOK
	if status == "pending" {
		code = http.StatusAccepted
	}
	common.WriteJSON(w, code, map[string]string{"ref": ref, "status": status})
}

func (s *Server) hTransfers(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT ref, direction, amount, status, created_at FROM fut_transfers WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, uid, limitParam(r, 50, 200))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ref, dir, st string
		var amt decimal.Decimal
		var at time.Time
		if rows.Scan(&ref, &dir, &amt, &st, &at) == nil {
			out = append(out, map[string]any{"ref": ref, "direction": dir, "amount": amt, "status": st, "createdAt": at})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"transfers": out})
}

func (s *Server) hGetLeverage(w http.ResponseWriter, r *http.Request, uid string) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	lev, mode, err := s.settings(r.Context(), s.db, uid, c)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "leverage": lev, "marginMode": mode, "maxLeverage": c.MaxLeverage, "positionMode": "one_way"})
}

func (s *Server) hSetLeverage(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Symbol     string `json:"symbol"`
		Leverage   int    `json:"leverage"`
		MarginMode string `json:"marginMode"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.SetLeverage(r.Context(), uid, req.Symbol, req.Leverage, strings.ToLower(req.MarginMode)); err != nil {
		writeErr(w, err)
		return
	}
	r.URL.RawQuery = "symbol=" + req.Symbol
	s.hGetLeverage(w, r, uid)
}

func (s *Server) hPlace(w http.ResponseWriter, r *http.Request, uid string) {
	var req PlaceReq
	if !decode(w, r, &req) {
		return
	}
	o, trades, err := s.PlaceOrder(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	if trades == nil {
		trades = []Trade{}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": o, "trades": trades})
}

func (s *Server) hCancel(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, errNotFound)
		return
	}
	o, err := s.CancelOrder(r.Context(), uid, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": o})
}

func (s *Server) hOrders(w http.ResponseWriter, r *http.Request, uid string) {
	q := `SELECT ` + orderCols + ` FROM fut_orders WHERE user_id=$1`
	args := []any{uid}
	if sym := r.URL.Query().Get("symbol"); sym != "" {
		args = append(args, strings.ToUpper(sym))
		q += ` AND symbol=$` + strconv.Itoa(len(args))
	}
	if r.URL.Query().Get("status") == "open" {
		q += ` AND status IN ('new','partially_filled')`
	}
	args = append(args, limitParam(r, 50, 500))
	q += ` ORDER BY id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, o)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"orders": out})
}

func (s *Server) hTPSL(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		TP *string `json:"tpPrice"`
		SL *string `json:"slPrice"`
	}
	if !decode(w, r, &req) {
		return
	}
	var tp, sl decimal.NullDecimal
	var clearTP, clearSL bool
	var err error
	if req.TP != nil {
		if tp, err = optDec(*req.TP); err != nil {
			writeErr(w, err)
			return
		}
		clearTP = !tp.Valid
	}
	if req.SL != nil {
		if sl, err = optDec(*req.SL); err != nil {
			writeErr(w, err)
			return
		}
		clearSL = !sl.Valid
	}
	if err := s.SetTPSL(r.Context(), uid, r.PathValue("symbol"), tp, sl, clearTP, clearSL); err != nil {
		writeErr(w, err)
		return
	}
	s.hPositions(w, r, uid)
}

func (s *Server) hMargin(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Amount string `json:"amount"` // >0 add, <0 remove
	}
	if !decode(w, r, &req) {
		return
	}
	d, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil || d.IsZero() || d.Exponent() < -8 {
		writeErr(w, bad("INVALID_AMOUNT", "金额不合法"))
		return
	}
	if err := s.AdjustMargin(r.Context(), uid, r.PathValue("symbol"), d); err != nil {
		writeErr(w, err)
		return
	}
	s.hPositions(w, r, uid)
}

func (s *Server) hClose(w http.ResponseWriter, r *http.Request, uid string) {
	o, trades, err := s.ClosePosition(r.Context(), uid, r.PathValue("symbol"), "user")
	if err != nil {
		writeErr(w, err)
		return
	}
	if trades == nil {
		trades = []Trade{}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": o, "trades": trades})
}

func (s *Server) hBills(w http.ResponseWriter, r *http.Request, uid string) {
	q := `SELECT id, COALESCE(symbol,''), kind, amount, COALESCE(ref,''), created_at FROM fut_bills WHERE user_id=$1`
	args := []any{uid}
	if k := r.URL.Query().Get("kind"); k != "" {
		args = append(args, k)
		q += ` AND kind=$2`
	}
	args = append(args, limitParam(r, 100, 1000))
	q += ` ORDER BY id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var sym, kind, ref string
		var amt decimal.Decimal
		var at time.Time
		if rows.Scan(&id, &sym, &kind, &amt, &ref, &at) == nil {
			out = append(out, map[string]any{"id": id, "symbol": sym, "kind": kind, "amount": amt, "ref": ref, "time": at})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"bills": out})
}

func (s *Server) hMyTrades(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, symbol, price, qty, taker_side, maker_user, maker_fee, taker_fee, created_at FROM fut_trades
		WHERE maker_user=$1 OR taker_user=$1 ORDER BY id DESC LIMIT $2`, uid, limitParam(r, 50, 500))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var sym, ts, mu string
		var px, qty, mf, tf decimal.Decimal
		var at time.Time
		if rows.Scan(&id, &sym, &px, &qty, &ts, &mu, &mf, &tf, &at) != nil {
			continue
		}
		side, fee, role := ts, tf, "taker"
		if mu == uid {
			role, fee = "maker", mf
			side = map[string]string{"buy": "sell", "sell": "buy"}[ts]
		}
		out = append(out, map[string]any{"tradeId": id, "symbol": sym, "side": side, "price": px, "qty": qty, "fee": fee, "role": role, "time": at})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"trades": out})
}

// ---- dev ----

func (s *Server) hDevIndex(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"` // "" clears the override
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.contract(req.Symbol)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pmu.Lock()
	if req.Price == "" {
		delete(s.devIndex, c.Symbol)
	} else if p, err := decimal.NewFromString(req.Price); err == nil && p.Sign() > 0 {
		s.devIndex[c.Symbol] = p
	}
	s.pmu.Unlock()
	s.Tick(r.Context(), false)
	s.hDevSnap(w, c.Symbol)
}

func (s *Server) hDevSnap(w http.ResponseWriter, sym string) {
	ps := s.snapshot(sym)
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": sym, "indexPrice": ps.Index, "markPrice": ps.Mark, "lastPrice": ps.Last})
}

func (s *Server) hDevTick(w http.ResponseWriter, r *http.Request) {
	s.Tick(r.Context(), true)
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// hDevFunding settles funding now; rate "" uses the predicted rate.
func (s *Server) hDevFunding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Symbol string `json:"symbol"`
		Rate   string `json:"rate"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.contract(req.Symbol)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Tick(r.Context(), false)
	ps := s.snapshot(c.Symbol)
	rate := s.predictedRate(ps)
	if req.Rate != "" {
		if rate, err = decimal.NewFromString(req.Rate); err != nil {
			writeErr(w, bad("VALIDATION_ERROR", "rate 不合法"))
			return
		}
	}
	if ps.Mark.Sign() <= 0 {
		writeErr(w, errMarkUnavail)
		return
	}
	s.mu.Lock()
	err = s.settleFunding(r.Context(), c.Symbol, rate, ps.Mark, s.now().UTC().Truncate(time.Millisecond))
	s.mu.Unlock()
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": c.Symbol, "rate": rate, "markPrice": ps.Mark})
}

// hDevAudit checks conservation: Σ equity over every account (system ones
// included) must equal net spot↔futures transfers (pending outs already left).
func (s *Server) hDevAudit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(r.Context(), `SELECT user_id FROM fut_accounts`)
	if err != nil {
		writeErr(w, err)
		return
	}
	var users []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			users = append(users, u)
		}
	}
	rows.Close()
	total := dZero
	for _, u := range users {
		st, err := s.state(r.Context(), s.db, u)
		if err != nil {
			writeErr(w, err)
			return
		}
		total = total.Add(st.Equity)
	}
	var net decimal.Decimal
	if err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(sum(CASE WHEN direction='in' THEN amount ELSE -amount END),0)
		FROM fut_transfers WHERE (direction='in' AND status='done') OR (direction='out' AND status<>'failed')`).Scan(&net); err != nil {
		writeErr(w, err)
		return
	}
	fee, _, _ := readAccount(r.Context(), s.db, feeUser)
	common.WriteJSON(w, http.StatusOK, map[string]any{"totalEquity": money(total), "netTransfers": money(net), "diff": total.Sub(net).Round(8), "feeBalance": money(fee), "accounts": len(users)})
}
