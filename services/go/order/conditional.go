package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Conditional spot orders: stop (stop-loss), take_profit and generic trigger
// orders. Each one carries a limit or market child order that is placed through
// the normal placement path once the last traded price crosses triggerPrice.
//
// Direction semantics:
//   up   = fire when last >= triggerPrice
//   down = fire when last <= triggerPrice
//   stop:        buy → up,   sell → down
//   take_profit: buy → down, sell → up
//   trigger:     derived from last price vs trigger (or explicit triggerDirection)

const maxPendingConditional = 50

type condCreateRequest struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	Type             string  `json:"type"` // limit | market (of the child order)
	Kind             string  `json:"kind"` // stop | take_profit | trigger (default trigger)
	TriggerPrice     string  `json:"triggerPrice"`
	TriggerDirection *string `json:"triggerDirection"`
	Price            *string `json:"price"`
	Quantity         *string `json:"quantity"`
	QuoteOrderQty    *string `json:"quoteOrderQty"`
	TimeInForce      *string `json:"timeInForce"`
	ClientOrderID    *string `json:"clientOrderId"`
}

type condDTO struct {
	ID            string  `json:"id"`
	ClientOrderID *string `json:"clientOrderId"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Kind          string  `json:"kind"`
	Type          string  `json:"type"`
	TriggerPrice  string  `json:"triggerPrice"`
	Direction     string  `json:"triggerDirection"`
	Price         *string `json:"price"`
	Quantity      *string `json:"quantity"`
	QuoteOrderQty *string `json:"quoteOrderQty"`
	TimeInForce   string  `json:"timeInForce"`
	Status        string  `json:"status"`
	OrderID       *string `json:"orderId"`
	FrozenAsset   string  `json:"frozenAsset"`
	FrozenAmount  string  `json:"frozenAmount"`
	FailReason    string  `json:"failReason"`
	CreatedAt     string  `json:"createdAt"`
	TriggeredAt   *string `json:"triggeredAt"`
}

type condRow struct {
	ID                         int64
	UserID, Symbol, Side, Kind string
	OrderType, Direction, TIF  string
	TriggerPrice               decimal.Decimal
	Price, Quantity, QuoteQty  sql.NullString
	ClientOrderID, OrderID     sql.NullString
	Status, FrozenAsset        string
	FrozenAmount               decimal.Decimal
	FailReason                 string
	CreatedAt                  time.Time
	TriggeredAt                sql.NullTime
}

const condCols = `id, user_id, symbol, side, kind, order_type, direction, tif, trigger_price, price::text, quantity::text, quote_qty::text,
	client_order_id, order_id, status, frozen_asset, frozen_amount, fail_reason, created_at, triggered_at`

func scanCond(sc rowScanner) (*condRow, error) {
	var c condRow
	err := sc.Scan(&c.ID, &c.UserID, &c.Symbol, &c.Side, &c.Kind, &c.OrderType, &c.Direction, &c.TIF, &c.TriggerPrice,
		&c.Price, &c.Quantity, &c.QuoteQty, &c.ClientOrderID, &c.OrderID, &c.Status, &c.FrozenAsset, &c.FrozenAmount,
		&c.FailReason, &c.CreatedAt, &c.TriggeredAt)
	return &c, err
}

func nsPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	if d, err := decimal.NewFromString(v); err == nil {
		v = d.String()
	}
	return &v
}

func (c *condRow) dto() condDTO {
	d := condDTO{ID: strconv.FormatInt(c.ID, 10), ClientOrderID: nsPtr(c.ClientOrderID), Symbol: c.Symbol, Side: c.Side,
		Kind: c.Kind, Type: c.OrderType, TriggerPrice: c.TriggerPrice.String(), Direction: c.Direction,
		Price: nsPtr(c.Price), Quantity: nsPtr(c.Quantity), QuoteOrderQty: nsPtr(c.QuoteQty), TimeInForce: c.TIF,
		Status: c.Status, FrozenAsset: c.FrozenAsset, FrozenAmount: c.FrozenAmount.String(), FailReason: c.FailReason,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339)}
	if c.OrderID.Valid {
		v := c.OrderID.String
		d.OrderID = &v
	}
	if c.TriggeredAt.Valid {
		v := c.TriggeredAt.Time.UTC().Format(time.RFC3339)
		d.TriggeredAt = &v
	}
	return d
}

func (c *condRow) childRequest() placeOrderRequest {
	tif := c.TIF
	req := placeOrderRequest{Symbol: c.Symbol, Side: c.Side, Type: c.OrderType, TimeInForce: &tif}
	if c.Price.Valid {
		v := c.Price.String
		req.Price = &v
	}
	if c.Quantity.Valid {
		v := c.Quantity.String
		req.Quantity = &v
	}
	if c.QuoteQty.Valid {
		v := c.QuoteQty.String
		req.QuoteOrderQty = &v
	}
	return req
}

// lastPrice is the most recent spot trade price for a symbol.
func (s *Server) lastPrice(ctx context.Context, symbol string) (decimal.Decimal, bool) {
	var p decimal.Decimal
	err := s.store.db.QueryRowContext(ctx, `SELECT price FROM trades WHERE symbol=$1 ORDER BY id DESC LIMIT 1`, symbol).Scan(&p)
	if err != nil || p.Sign() <= 0 {
		return decimal.Zero, false
	}
	return p, true
}

func crossed(dir string, last, trig decimal.Decimal) bool {
	if dir == "up" {
		return last.GreaterThanOrEqual(trig)
	}
	return last.LessThanOrEqual(trig)
}

func (s *Server) handleCondCreate(w http.ResponseWriter, r *http.Request, userID string) {
	var req condCreateRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	ctx := r.Context()
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "trigger"
	}
	if kind == "stop_loss" || kind == "sl" {
		kind = "stop"
	}
	if kind == "tp" {
		kind = "take_profit"
	}
	if kind != "stop" && kind != "take_profit" && kind != "trigger" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "kind 必须为 stop/take_profit/trigger")
		return
	}
	trig, err := decimal.NewFromString(strings.TrimSpace(req.TriggerPrice))
	if err != nil || trig.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "triggerPrice 不合法")
		return
	}
	side := strings.ToLower(req.Side)
	child := placeOrderRequest{Symbol: req.Symbol, Side: req.Side, Type: req.Type, Price: req.Price,
		Quantity: req.Quantity, QuoteOrderQty: req.QuoteOrderQty, TimeInForce: req.TimeInForce}
	// Market children are budgeted at the trigger price for validation/freeze
	// when no limit price is supplied (buildOrder needs one for market buys by qty).
	o, sym, code, msg := s.buildOrder(ctx, userID, child)
	if code != "" {
		st := http.StatusBadRequest
		if code == "INTERNAL" {
			st = http.StatusInternalServerError
		}
		common.WriteError(w, st, code, msg)
		return
	}
	if !trig.Equal(trig.Round(int32(sym.PricePrecision))) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "triggerPrice 精度超出限制")
		return
	}
	last, haveLast := s.lastPrice(ctx, sym.Symbol)
	var dir string
	switch kind {
	case "stop":
		dir = map[string]string{"buy": "up", "sell": "down"}[side]
	case "take_profit":
		dir = map[string]string{"buy": "down", "sell": "up"}[side]
	default:
		if req.TriggerDirection != nil && *req.TriggerDirection != "" {
			dir = strings.ToLower(*req.TriggerDirection)
		} else if haveLast {
			dir = "up"
			if trig.LessThan(last) {
				dir = "down"
			}
		}
	}
	if dir != "up" && dir != "down" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无法确定触发方向，请传 triggerDirection=up|down")
		return
	}
	if haveLast && crossed(dir, last, trig) {
		common.WriteError(w, http.StatusBadRequest, "TRIGGER_WOULD_IMMEDIATELY_FIRE", "触发价已被当前价格穿越，将立即触发")
		return
	}
	var pending int
	_ = s.store.db.QueryRowContext(ctx, `SELECT count(*) FROM spot_conditional_orders WHERE user_id=$1 AND status IN ('pending','triggering')`, userID).Scan(&pending)
	if pending >= maxPendingConditional {
		common.WriteError(w, http.StatusBadRequest, "CONDITIONAL_LIMIT_EXCEEDED", "条件单数量已达上限")
		return
	}
	if req.ClientOrderID != nil && *req.ClientOrderID != "" {
		row := s.store.db.QueryRowContext(ctx, `SELECT `+condCols+` FROM spot_conditional_orders WHERE user_id=$1 AND client_order_id=$2`, userID, *req.ClientOrderID)
		if c, err := scanCond(row); err == nil {
			common.WriteJSON(w, http.StatusOK, c.dto()) // idempotent replay
			return
		}
	}
	var price, qty, qq any
	if o.HasPrice {
		price = o.Price.String()
	}
	if o.Quantity.Sign() > 0 {
		qty = o.Quantity.String()
	}
	if o.HasQuoteQty {
		qq = o.QuoteOrderQty.String()
	}
	var coid any
	if req.ClientOrderID != nil && *req.ClientOrderID != "" {
		coid = *req.ClientOrderID
	}
	row := s.store.db.QueryRowContext(ctx, `INSERT INTO spot_conditional_orders
		(user_id, symbol, side, kind, order_type, trigger_price, direction, price, quantity, quote_qty, tif, client_order_id, frozen_asset, frozen_amount)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+condCols,
		userID, sym.Symbol, o.Side, kind, o.Type, trig, dir, price, qty, qq, o.TimeInForce, coid, o.FrozenAsset, o.FrozenAmount)
	c, err := scanCond(row)
	if err != nil {
		if strings.Contains(err.Error(), "spot_cond_client") {
			common.WriteError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "clientOrderId 重复")
			return
		}
		log.Printf("cond create: insert: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	ref := "cond:" + strconv.FormatInt(c.ID, 10)
	if err := s.asset.Freeze(ctx, userID, c.FrozenAsset, c.FrozenAmount, ref); err != nil {
		reason, codeOut, st := "freeze_failed", "INTERNAL", http.StatusInternalServerError
		if errors.Is(err, ErrInsufficient) {
			reason, codeOut, st = "insufficient_balance", "INSUFFICIENT_BALANCE", http.StatusBadRequest
		}
		_, _ = s.store.db.ExecContext(ctx, `UPDATE spot_conditional_orders SET status='failed', fail_reason=$2, updated_at=now() WHERE id=$1`, c.ID, reason)
		if st == http.StatusBadRequest {
			common.WriteError(w, st, codeOut, "余额不足")
		} else {
			log.Printf("cond create: freeze: %v", err)
			common.WriteError(w, st, codeOut, "服务器内部错误")
		}
		return
	}
	common.WriteJSON(w, http.StatusCreated, c.dto())
}

func (s *Server) handleCondList(w http.ResponseWriter, r *http.Request, userID string) {
	q := r.URL.Query()
	where := []string{"user_id=$1"}
	args := []any{userID}
	if sym := strings.ToUpper(q.Get("symbol")); sym != "" {
		args = append(args, sym)
		where = append(where, "symbol=$"+strconv.Itoa(len(args)))
	}
	switch st := q.Get("status"); st {
	case "", "open":
		if st == "open" || q.Get("status") == "" {
			where = append(where, "status IN ('pending','triggering')")
		}
	case "all":
	default:
		args = append(args, st)
		where = append(where, "status=$"+strconv.Itoa(len(args)))
	}
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM spot_conditional_orders WHERE `+cond, args...).Scan(&total); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT `+condCols+` FROM spot_conditional_orders WHERE `+cond+
		` ORDER BY id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []condDTO{}
	for rows.Next() {
		c, err := scanCond(rows)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items = append(items, c.dto())
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (s *Server) handleCondCancel(w http.ResponseWriter, r *http.Request, userID string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "条件单不存在")
		return
	}
	ctx := r.Context()
	row := s.store.db.QueryRowContext(ctx, `UPDATE spot_conditional_orders SET status='canceled', updated_at=now()
		WHERE id=$1 AND user_id=$2 AND status='pending' RETURNING `+condCols, id, userID)
	c, err := scanCond(row)
	if errors.Is(err, sql.ErrNoRows) {
		row = s.store.db.QueryRowContext(ctx, `SELECT `+condCols+` FROM spot_conditional_orders WHERE id=$1 AND user_id=$2`, id, userID)
		ex, err2 := scanCond(row)
		if err2 != nil {
			common.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "条件单不存在")
			return
		}
		if ex.Status == "canceled" {
			common.WriteJSON(w, http.StatusOK, ex.dto())
			return
		}
		common.WriteError(w, http.StatusConflict, "ORDER_NOT_CANCELABLE", "条件单已触发或已结束")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.releaseCond(ctx, c)
	common.WriteJSON(w, http.StatusOK, c.dto())
}

func (s *Server) releaseCond(ctx context.Context, c *condRow) error {
	ref := "cond:" + strconv.FormatInt(c.ID, 10) + ":release"
	err := s.asset.Unfreeze(ctx, c.UserID, c.FrozenAsset, c.FrozenAmount, ref)
	if err != nil {
		log.Printf("cond %d: unfreeze: %v", c.ID, err)
	}
	return err
}

// RunBackground drives the conditional-order trigger loop and periodic
// fee-override refresh.
func (s *Server) RunBackground(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.triggerPass(ctx)
		if n++; n%30 == 0 {
			_ = s.LoadFees(ctx)
			_ = s.LoadUserFees(ctx)
		}
	}
}

// triggerPass fires every pending conditional order whose trigger has crossed.
// A row is claimed (pending→triggering) before anything else, so concurrent
// replicas never fire one twice.
func (s *Server) triggerPass(ctx context.Context) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM spot_conditional_orders WHERE status IN ('pending','triggering')`)
	if err != nil {
		return
	}
	var syms []string
	for rows.Next() {
		var sym string
		if rows.Scan(&sym) == nil {
			syms = append(syms, sym)
		}
	}
	rows.Close()
	s.recoverStaleCond(ctx)
	for _, sym := range syms {
		last, ok := s.lastPrice(ctx, sym)
		if !ok {
			continue
		}
		crows, err := s.store.db.QueryContext(ctx, `UPDATE spot_conditional_orders SET status='triggering', updated_at=now()
			WHERE symbol=$1 AND status='pending' AND ((direction='up' AND $2 >= trigger_price) OR (direction='down' AND $2 <= trigger_price))
			RETURNING `+condCols, sym, last)
		if err != nil {
			log.Printf("cond trigger: claim: %v", err)
			continue
		}
		var fire []*condRow
		for crows.Next() {
			if c, err := scanCond(crows); err == nil {
				fire = append(fire, c)
			}
		}
		crows.Close()
		for _, c := range fire {
			s.fireCond(ctx, c)
		}
	}
}

func (s *Server) fireCond(ctx context.Context, c *condRow) {
	if err := s.releaseCond(ctx, c); err != nil {
		return // stays triggering; retried later (unfreeze ref is idempotent)
	}
	dto, perr := s.placeOrder(ctx, c.UserID, c.childRequest())
	if perr != nil {
		_, _ = s.store.db.ExecContext(ctx, `UPDATE spot_conditional_orders SET status='failed', fail_reason=$2, triggered_at=now(), updated_at=now() WHERE id=$1`,
			c.ID, perr.code+": "+perr.msg)
		return
	}
	_, _ = s.store.db.ExecContext(ctx, `UPDATE spot_conditional_orders SET status='triggered', order_id=$2, triggered_at=now(), updated_at=now() WHERE id=$1`,
		c.ID, dto.OrderID)
}

// recoverStaleCond handles rows stuck in triggering (process died mid-fire).
// Re-placing could duplicate an order, so if a child order already exists for
// the user/symbol since the claim we can't tell; we fail the row instead and
// make sure its funds are released (the release ref is idempotent).
func (s *Server) recoverStaleCond(ctx context.Context) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+condCols+` FROM spot_conditional_orders
		WHERE status='triggering' AND updated_at < now() - interval '60 seconds' LIMIT 50`)
	if err != nil {
		return
	}
	var stale []*condRow
	for rows.Next() {
		if c, err := scanCond(rows); err == nil {
			stale = append(stale, c)
		}
	}
	rows.Close()
	for _, c := range stale {
		if s.releaseCond(ctx, c) != nil {
			continue
		}
		_, _ = s.store.db.ExecContext(ctx, `UPDATE spot_conditional_orders SET status='failed', fail_reason='trigger_interrupted', updated_at=now()
			WHERE id=$1 AND status='triggering'`, c.ID)
	}
}
