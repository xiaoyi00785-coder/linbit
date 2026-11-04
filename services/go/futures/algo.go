package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// algo.go: algo orders kept off-book until a price watcher fires them:
//   - tp / sl: reduce-only close of the whole position (qty NULL) or a part of it
//   - plan:    conditional order (any side, limit or market) placed when the trigger price is reached
// Triggers compare against the LAST (last trade; mark when there is no trade yet)
// or MARK price on every engine tick. Nothing is reserved until the order is placed.

type AlgoOrder struct {
	ID          int64               `json:"algoId"`
	UserID      string              `json:"-"`
	Symbol      string              `json:"symbol"`
	Kind        string              `json:"algoType"` // tp | sl | plan
	Side        string              `json:"side"`
	PosSide     string              `json:"positionSide"`
	Type        string              `json:"orderType"`
	Price       decimal.NullDecimal `json:"price"`
	Qty         decimal.NullDecimal `json:"qty"` // null = whole position
	Trigger     decimal.Decimal     `json:"triggerPrice"`
	TriggerType string              `json:"triggerPriceType"`
	Dir         string              `json:"triggerDirection"`
	ReduceOnly  bool                `json:"reduceOnly"`
	TIF         string              `json:"timeInForce"`
	Status      string              `json:"status"`
	OrderID     sql.NullInt64       `json:"-"`
	OrderRef    *int64              `json:"orderId"`
	FailReason  sql.NullString      `json:"-"`
	Reason      string              `json:"failReason,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	TriggeredAt sql.NullTime        `json:"-"`
	TrigAt      *time.Time          `json:"triggeredAt"`
}

const algoCols = `id, user_id, symbol, kind, side, position_side, order_type, price, qty, trigger_price, trigger_type, trigger_dir, reduce_only, tif, status, order_id, fail_reason, created_at, updated_at, triggered_at`

func scanAlgo(sc interface{ Scan(...any) error }) (AlgoOrder, error) {
	var a AlgoOrder
	err := sc.Scan(&a.ID, &a.UserID, &a.Symbol, &a.Kind, &a.Side, &a.PosSide, &a.Type, &a.Price, &a.Qty, &a.Trigger, &a.TriggerType, &a.Dir, &a.ReduceOnly, &a.TIF, &a.Status, &a.OrderID, &a.FailReason, &a.CreatedAt, &a.UpdatedAt, &a.TriggeredAt)
	if err == nil {
		if a.OrderID.Valid {
			v := a.OrderID.Int64
			a.OrderRef = &v
		}
		if a.TriggeredAt.Valid {
			t := a.TriggeredAt.Time
			a.TrigAt = &t
		}
		a.Reason = a.FailReason.String
	}
	return a, err
}

// triggerRef is the price a trigger of type tt compares against.
func (s *Server) triggerRef(sym, tt string) (decimal.Decimal, bool) {
	m, ok := s.mark(sym)
	if tt == "MARK" {
		return m, ok
	}
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	if ps := s.prices[sym]; ps != nil && ps.Last.Sign() > 0 {
		return ps.Last, true
	}
	return m, ok
}

func normTrigger(t, def string) (string, error) {
	t = strings.ToUpper(strings.TrimSpace(t))
	switch t {
	case "":
		return def, nil
	case "LAST", "CONTRACT_PRICE", "LAST_PRICE":
		return "LAST", nil
	case "MARK", "MARK_PRICE":
		return "MARK", nil
	}
	return "", bad("VALIDATION_ERROR", "triggerPriceType 只能是 LAST 或 MARK")
}

func fires(dir string, ref, trig decimal.Decimal) bool {
	if dir == "up" {
		return ref.GreaterThanOrEqual(trig)
	}
	return ref.LessThanOrEqual(trig)
}

func (s *Server) checkAlgoLimit(ctx context.Context, tx *sql.Tx, uid, sym string, adding int, cfg OrderConfig) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM fut_algo_orders WHERE user_id=$1 AND symbol=$2 AND status='active'`, uid, sym).Scan(&n); err != nil {
		return err
	}
	if n+adding > cfg.MaxAlgoPerSymbol {
		return bad("TOO_MANY_ALGO_ORDERS", fmt.Sprintf("每个合约最多 %d 个止盈止损/计划委托", cfg.MaxAlgoPerSymbol))
	}
	return nil
}

func insertAlgo(ctx context.Context, tx *sql.Tx, a AlgoOrder) (AlgoOrder, error) {
	return scanAlgo(tx.QueryRowContext(ctx, `INSERT INTO fut_algo_orders (user_id, symbol, kind, side, position_side, order_type, price, qty, trigger_price, trigger_type, trigger_dir, reduce_only, tif)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+algoCols,
		a.UserID, a.Symbol, a.Kind, a.Side, a.PosSide, a.Type, a.Price, a.Qty, a.Trigger, a.TriggerType, a.Dir, a.ReduceOnly, a.TIF))
}

// ---- TP/SL orders ----

type TPSLReq struct {
	Symbol       string `json:"symbol"`
	PositionSide string `json:"positionSide"`
	Qty          string `json:"qty"` // empty = whole position (closes whatever is open at trigger time)
	TPTrigger    string `json:"tpTriggerPrice"`
	TPPrice      string `json:"tpPrice"` // empty = market
	TPType       string `json:"tpTriggerPriceType"`
	SLTrigger    string `json:"slTriggerPrice"`
	SLPrice      string `json:"slPrice"`
	SLType       string `json:"slTriggerPriceType"`
}

func (s *Server) CreateTPSL(ctx context.Context, uid string, r TPSLReq) ([]AlgoOrder, error) {
	c, err := s.contract(r.Symbol)
	if err != nil {
		return nil, err
	}
	side := strings.ToUpper(strings.TrimSpace(r.PositionSide))
	if side == "" {
		if side, err = s.defaultCloseSide(ctx, uid, c.Symbol); err != nil {
			return nil, err
		}
	}
	var out []AlgoOrder
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		cfg, err := loadOrderConfig(ctx, tx)
		if err != nil {
			return err
		}
		if mode, err := positionMode(ctx, tx, uid); err != nil {
			return err
		} else if (mode == "HEDGE") != (side != "BOTH") {
			return bad("POSITION_SIDE_MISMATCH", "positionSide 与当前持仓模式不符")
		}
		p, _, err := loadPosition(ctx, tx, uid, c.Symbol, side, false)
		if err != nil {
			return err
		}
		if p.Qty.IsZero() {
			return bad("NO_POSITION", "当前无持仓")
		}
		long := p.Qty.Sign() > 0
		closeSide := "sell"
		if !long {
			closeSide = "buy"
		}
		var qty decimal.NullDecimal
		if q := strings.TrimSpace(r.Qty); q != "" {
			d, err := decimal.NewFromString(q)
			if err != nil || d.Sign() <= 0 || !isStep(d, c.MinQty) {
				return bad("INVALID_QTY", fmt.Sprintf("数量须为 %s 的整数倍", c.MinQty))
			}
			if d.GreaterThan(p.Qty.Abs()) {
				return bad("INVALID_QTY", "止盈止损数量不能超过持仓数量")
			}
			qty = decimal.NullDecimal{Decimal: d, Valid: true}
		}
		type leg struct {
			kind, trig, px, tt string
		}
		legs := []leg{{"tp", r.TPTrigger, r.TPPrice, r.TPType}, {"sl", r.SLTrigger, r.SLPrice, r.SLType}}
		n := 0
		for _, l := range legs {
			if strings.TrimSpace(l.trig) != "" {
				n++
			}
		}
		if n == 0 {
			return bad("VALIDATION_ERROR", "请填写止盈或止损触发价")
		}
		// 全仓止盈止损是"设置/修改"语义：同一持仓方向上已有的全仓 tp/sl 由新的替换（先撤旧，再挂新），
		// 而不是叠加出第二组。部分数量的止盈止损仍可多组并存。
		if !qty.Valid {
			var kinds []string
			for _, l := range legs {
				if strings.TrimSpace(l.trig) != "" {
					kinds = append(kinds, l.kind)
				}
			}
			if err := cancelPositionTPSL(ctx, tx, uid, c.Symbol, side, kinds); err != nil {
				return err
			}
		}
		if err := s.checkAlgoLimit(ctx, tx, uid, c.Symbol, n, cfg); err != nil {
			return err
		}
		for _, l := range legs {
			if strings.TrimSpace(l.trig) == "" {
				continue
			}
			tt, err := normTrigger(l.tt, cfg.TPSLDefaultTrigger)
			if err != nil {
				return err
			}
			trig, err := decimal.NewFromString(strings.TrimSpace(l.trig))
			if err != nil || trig.Sign() <= 0 || !isStep(trig, c.Tick) {
				return bad("INVALID_PRICE", fmt.Sprintf("触发价须为 %s 的整数倍", c.Tick))
			}
			ref, ok := s.triggerRef(c.Symbol, tt)
			if !ok {
				return errMarkUnavail
			}
			// TP above (long) / below (short) the reference; SL the opposite.
			up := (l.kind == "tp") == long
			if (up && !trig.GreaterThan(ref)) || (!up && !trig.LessThan(ref)) {
				return bad("INVALID_TPSL", "止盈/止损触发价方向不正确")
			}
			a := AlgoOrder{UserID: uid, Symbol: c.Symbol, Kind: l.kind, Side: closeSide, PosSide: side, Type: "market", Qty: qty,
				Trigger: trig, TriggerType: tt, Dir: "down", ReduceOnly: true, TIF: "GTC"}
			if up {
				a.Dir = "up"
			}
			if px := strings.TrimSpace(l.px); px != "" && px != "0" {
				d, err := decimal.NewFromString(px)
				if err != nil || d.Sign() <= 0 || !isStep(d, c.Tick) {
					return bad("INVALID_PRICE", fmt.Sprintf("价格须为 %s 的整数倍", c.Tick))
				}
				a.Type, a.Price = "limit", decimal.NullDecimal{Decimal: d, Valid: true}
			}
			a, err = insertAlgo(ctx, tx, a)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return nil
	})
	return out, err
}

// ---- plan (conditional) orders ----

func (s *Server) CreatePlan(ctx context.Context, uid string, r PlaceReq) (AlgoOrder, error) {
	c, err := s.contract(r.Symbol)
	if err != nil {
		return AlgoOrder{}, err
	}
	if c.Status != "trading" {
		return AlgoOrder{}, errContractHalted
	}
	side, typ, tif := strings.ToLower(r.Side), strings.ToLower(r.Type), strings.ToUpper(r.TIF)
	if side != "buy" && side != "sell" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "side 必须为 buy 或 sell")
	}
	if typ == "" {
		typ = "market"
	}
	if typ != "limit" && typ != "market" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "type 必须为 limit 或 market")
	}
	if tif == "" {
		tif = "GTC"
	}
	if tif == "GTX" {
		tif = "POST_ONLY"
	}
	if tif != "GTC" && tif != "IOC" && tif != "POST_ONLY" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "timeInForce 仅支持 GTC/IOC/POST_ONLY")
	}
	if tif == "POST_ONLY" && typ != "limit" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "只做 Maker（POST_ONLY）仅支持限价单")
	}
	if strings.TrimSpace(r.TP) != "" || strings.TrimSpace(r.SL) != "" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "计划委托暂不支持附带止盈止损，请在成交后设置")
	}
	qty, err := checkQty(c, r.Qty)
	if err != nil {
		return AlgoOrder{}, err
	}
	var price decimal.NullDecimal
	if typ == "limit" {
		p, err := checkPrice(c, r.Price, "价格")
		if err != nil {
			return AlgoOrder{}, err
		}
		price = decimal.NullDecimal{Decimal: p, Valid: true}
	}
	trig, err := checkPrice(c, r.TriggerPrice, "触发价")
	if err != nil {
		return AlgoOrder{}, err
	}
	var a AlgoOrder
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		cfg, err := loadOrderConfig(ctx, tx)
		if err != nil {
			return err
		}
		if tif == "POST_ONLY" && !cfg.PostOnlyEnabled {
			return bad("POST_ONLY_DISABLED", "只做 Maker 暂未开放")
		}
		tt, err := normTrigger(r.TriggerType, cfg.PlanDefaultTrigger)
		if err != nil {
			return err
		}
		posSide, reduce, err := s.resolvePosSide(ctx, tx, uid, r.PositionSide, side, r.ReduceOnly)
		if err != nil {
			return err
		}
		ref, ok := s.triggerRef(c.Symbol, tt)
		if !ok {
			return errMarkUnavail
		}
		if trig.Equal(ref) {
			return bad("INVALID_TRIGGER", "触发价不能等于当前价格")
		}
		if trig.Sub(ref).Abs().Div(ref).GreaterThan(cfg.PlanMaxDeviation) {
			return bad("INVALID_TRIGGER", fmt.Sprintf("触发价偏离当前价格超过 %s%%", cfg.PlanMaxDeviation.Mul(decimal.NewFromInt(100)).String()))
		}
		if err := s.checkAlgoLimit(ctx, tx, uid, c.Symbol, 1, cfg); err != nil {
			return err
		}
		dir := "down"
		if trig.GreaterThan(ref) {
			dir = "up"
		}
		a, err = insertAlgo(ctx, tx, AlgoOrder{UserID: uid, Symbol: c.Symbol, Kind: "plan", Side: side, PosSide: posSide, Type: typ, Price: price,
			Qty: decimal.NullDecimal{Decimal: qty, Valid: true}, Trigger: trig, TriggerType: tt, Dir: dir, ReduceOnly: reduce, TIF: tif})
		return err
	})
	return a, err
}

func (s *Server) CancelAlgo(ctx context.Context, uid string, id int64, kinds []string) (AlgoOrder, error) {
	var a AlgoOrder
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		a, err = scanAlgo(tx.QueryRowContext(ctx, `SELECT `+algoCols+` FROM fut_algo_orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, uid))
		if err == sql.ErrNoRows {
			return errNotFound
		}
		if err != nil {
			return err
		}
		okKind := false
		for _, k := range kinds {
			okKind = okKind || k == a.Kind
		}
		if !okKind {
			return errNotFound
		}
		if a.Status != "active" {
			return bad("ORDER_NOT_OPEN", "委托已触发或已撤销")
		}
		a, err = scanAlgo(tx.QueryRowContext(ctx, `UPDATE fut_algo_orders SET status='canceled', fail_reason='user', updated_at=now() WHERE id=$1 RETURNING `+algoCols, id))
		return err
	})
	return a, err
}

// ---- watcher ----

// triggerAlgos fires active algo orders whose trigger price was reached and
// expires TP/SL orders whose position is gone. Caller holds s.mu.
func (s *Server) triggerAlgos(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE fut_algo_orders a SET status='canceled', fail_reason='position_closed', updated_at=now()
		WHERE a.status='active' AND a.kind IN ('tp','sl') AND NOT EXISTS (SELECT 1 FROM fut_positions p WHERE p.user_id=a.user_id AND p.symbol=a.symbol AND p.position_side=a.position_side AND p.qty<>0)`)
	rows, err := s.db.QueryContext(ctx, `SELECT `+algoCols+` FROM fut_algo_orders WHERE status='active' ORDER BY id`)
	if err != nil {
		return
	}
	var list []AlgoOrder
	for rows.Next() {
		if a, err := scanAlgo(rows); err == nil {
			list = append(list, a)
		}
	}
	rows.Close()
	for _, a := range list {
		ref, ok := s.triggerRef(a.Symbol, a.TriggerType)
		if !ok || !fires(a.Dir, ref, a.Trigger) {
			continue
		}
		s.fireAlgo(ctx, a)
	}
}

func (s *Server) fireAlgo(ctx context.Context, a AlgoOrder) {
	// claim it first so a failure never fires twice
	res, err := s.db.ExecContext(ctx, `UPDATE fut_algo_orders SET status='triggered', triggered_at=now(), updated_at=now() WHERE id=$1 AND status='active'`, a.ID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	fail := func(reason string) {
		_, _ = s.db.ExecContext(ctx, `UPDATE fut_algo_orders SET status='failed', fail_reason=$2, updated_at=now() WHERE id=$1`, a.ID, reason)
	}
	req := PlaceReq{Symbol: a.Symbol, Side: a.Side, Type: a.Type, TIF: a.TIF, ReduceOnly: a.ReduceOnly, PositionSide: a.PosSide, source: a.Kind, algoID: a.ID}
	if a.Price.Valid {
		req.Price = a.Price.Decimal.String()
	}
	if a.Kind == "tp" || a.Kind == "sl" {
		p, _, err := loadPosition(ctx, s.db, a.UserID, a.Symbol, a.PosSide, false)
		if err != nil {
			fail("internal")
			return
		}
		q := p.Qty.Abs()
		if a.Qty.Valid {
			q = dmin(q, a.Qty.Decimal)
		}
		if q.IsZero() {
			_, _ = s.db.ExecContext(ctx, `UPDATE fut_algo_orders SET status='canceled', fail_reason='position_closed', updated_at=now() WHERE id=$1`, a.ID)
			return
		}
		req.Qty = q.String()
	} else {
		req.Qty = a.Qty.Decimal.String()
	}
	if a.PosSide == "BOTH" {
		// position mode cannot change while algo orders are active, so BOTH stays valid
		req.PositionSide = ""
	}
	o, _, err := s.placeLocked(ctx, a.UserID, req)
	if err != nil {
		code := "rejected"
		if ae, ok := asAPI(err); ok {
			code = ae.Code
		}
		log.Printf("futures: algo %d (%s %s) trigger failed: %v", a.ID, a.Kind, a.Symbol, err)
		fail(code)
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE fut_algo_orders SET order_id=$2, updated_at=now() WHERE id=$1`, a.ID, o.ID)
}

// ---- handlers ----

func (s *Server) hCreateTPSL(w http.ResponseWriter, r *http.Request, uid string) {
	var req TPSLReq
	if !decode(w, r, &req) {
		return
	}
	out, err := s.CreateTPSL(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"orders": out})
}

func (s *Server) hCreatePlan(w http.ResponseWriter, r *http.Request, uid string) {
	var req PlaceReq
	if !decode(w, r, &req) {
		return
	}
	s.hPlanResult(w, r, uid, req)
}

func (s *Server) hPlanResult(w http.ResponseWriter, r *http.Request, uid string, req PlaceReq) {
	a, err := s.CreatePlan(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": a})
}

func (s *Server) listAlgo(kinds ...string) userHandler {
	return func(w http.ResponseWriter, r *http.Request, uid string) {
		qs := `SELECT ` + algoCols + ` FROM fut_algo_orders WHERE user_id=$1 AND kind = ANY($2)`
		args := []any{uid, "{" + strings.Join(kinds, ",") + "}"}
		if sym := r.URL.Query().Get("symbol"); sym != "" {
			args = append(args, strings.ToUpper(sym))
			qs += ` AND symbol=$` + strconv.Itoa(len(args))
		}
		switch r.URL.Query().Get("status") {
		case "", "active", "open":
			qs += ` AND status='active'`
		case "history":
			qs += ` AND status<>'active'`
		}
		if v, err := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64); err == nil && v > 0 {
			args = append(args, v)
			qs += ` AND id<$` + strconv.Itoa(len(args))
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
		out := []AlgoOrder{}
		for rows.Next() {
			a, err := scanAlgo(rows)
			if err != nil {
				writeErr(w, err)
				return
			}
			out = append(out, a)
		}
		var next any
		if len(out) > lim {
			out = out[:lim]
			next = strconv.FormatInt(out[lim-1].ID, 10)
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"orders": out, "nextCursor": next})
	}
}

func (s *Server) cancelAlgo(kinds ...string) userHandler {
	return func(w http.ResponseWriter, r *http.Request, uid string) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, errNotFound)
			return
		}
		a, err := s.CancelAlgo(r.Context(), uid, id, kinds)
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"order": a})
	}
}

// cancelPositionTPSL cancels the active whole-position (qty IS NULL) tp/sl algo
// orders of the given kinds on one position side, so a new pair replaces them.
func cancelPositionTPSL(ctx context.Context, tx *sql.Tx, uid, sym, posSide string, kinds []string) error {
	if len(kinds) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE fut_algo_orders SET status='canceled', updated_at=now()
		WHERE user_id=$1 AND symbol=$2 AND position_side=$3 AND kind = ANY($4) AND qty IS NULL AND status='active'`,
		uid, sym, posSide, "{"+strings.Join(kinds, ",")+"}")
	return err
}
