package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// algo_amend.go: amend an active plan or TP/SL algo order in place (same algoId).
// Omitted / empty fields keep their current value.

type AlgoAmendReq struct {
	TriggerPrice string `json:"triggerPrice"`
	TriggerType  string `json:"triggerPriceType"` // LAST | MARK
	OrderType    string `json:"orderType"`        // limit | market; price given => limit
	Price        string `json:"price"`
	Qty          string `json:"qty"`
}

func (s *Server) AmendAlgo(ctx context.Context, uid string, id int64, kinds []string, r AlgoAmendReq) (AlgoOrder, error) {
	r.TriggerPrice, r.TriggerType, r.OrderType = strings.TrimSpace(r.TriggerPrice), strings.TrimSpace(r.TriggerType), strings.ToLower(strings.TrimSpace(r.OrderType))
	r.Price, r.Qty = strings.TrimSpace(r.Price), strings.TrimSpace(r.Qty)
	if r.TriggerPrice == "" && r.TriggerType == "" && r.OrderType == "" && r.Price == "" && r.Qty == "" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "至少修改一项：triggerPrice / triggerPriceType / orderType / price / qty")
	}
	if r.OrderType != "" && r.OrderType != "limit" && r.OrderType != "market" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "orderType 必须为 limit 或 market")
	}
	if r.OrderType == "market" && r.Price != "" {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "市价单不能填写 price")
	}
	// Same lock as the trigger watcher, so an order can't fire with stale values mid-amend.
	// withTx takes s.mu itself; s.mu is not reentrant, so do not lock it here as well
	// (double-locking deadlocked plan and TP/SL amend and froze the engine).
	var out AlgoOrder
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		a, err := scanAlgo(tx.QueryRowContext(ctx, `SELECT `+algoCols+` FROM fut_algo_orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, uid))
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
		c, err := s.contract(a.Symbol)
		if err != nil {
			return err
		}
		cfg, err := loadOrderConfig(ctx, tx)
		if err != nil {
			return err
		}
		if r.TriggerType != "" {
			if a.TriggerType, err = normTrigger(r.TriggerType, a.TriggerType); err != nil {
				return err
			}
		}
		if r.TriggerPrice != "" {
			t, err := decimal.NewFromString(r.TriggerPrice)
			if err != nil || t.Sign() <= 0 || !isStep(t, c.Tick) {
				return bad("INVALID_PRICE", fmt.Sprintf("触发价须为 %s 的整数倍", c.Tick))
			}
			a.Trigger = t
		}
		if r.OrderType == "market" {
			if a.TIF == "POST_ONLY" {
				return bad("VALIDATION_ERROR", "只做 Maker（POST_ONLY）仅支持限价单")
			}
			a.Type, a.Price = "market", decimal.NullDecimal{}
		}
		if r.Price != "" {
			p, err := decimal.NewFromString(r.Price)
			if err != nil || p.Sign() <= 0 || !isStep(p, c.Tick) {
				return bad("INVALID_PRICE", fmt.Sprintf("价格须为 %s 的整数倍", c.Tick))
			}
			a.Type, a.Price = "limit", decimal.NullDecimal{Decimal: p, Valid: true}
		}
		if a.Type == "limit" && !a.Price.Valid {
			return bad("INVALID_PRICE", "限价单须填写 price")
		}
		var posQty decimal.Decimal
		long := a.Side == "sell" // tp/sl close side: sell closes a long
		if a.Kind != "plan" {
			p, _, err := loadPosition(ctx, tx, uid, a.Symbol, a.PosSide, false)
			if err != nil {
				return err
			}
			if p.Qty.IsZero() {
				return bad("NO_POSITION", "当前无持仓")
			}
			posQty, long = p.Qty.Abs(), p.Qty.Sign() > 0
		}
		if r.Qty != "" {
			q, err := decimal.NewFromString(r.Qty)
			if err != nil || q.Sign() <= 0 || !isStep(q, c.MinQty) {
				return bad("INVALID_QTY", fmt.Sprintf("数量须为 %s 的整数倍", c.MinQty))
			}
			if a.Kind == "plan" && q.GreaterThan(c.MaxQty) {
				return bad("INVALID_QTY", "超过单笔最大数量")
			}
			if a.Kind != "plan" && q.GreaterThan(posQty) {
				return bad("INVALID_QTY", "止盈止损数量不能超过持仓数量")
			}
			a.Qty = decimal.NullDecimal{Decimal: q, Valid: true}
		}
		ref, ok := s.triggerRef(a.Symbol, a.TriggerType)
		if !ok {
			return errMarkUnavail
		}
		if a.Kind == "plan" {
			if a.Trigger.Equal(ref) {
				return bad("INVALID_TRIGGER", "触发价不能等于当前价格")
			}
			if a.Trigger.Sub(ref).Abs().Div(ref).GreaterThan(cfg.PlanMaxDeviation) {
				return bad("INVALID_TRIGGER", fmt.Sprintf("触发价偏离当前价格超过 %s%%", cfg.PlanMaxDeviation.Mul(decimal.NewFromInt(100)).String()))
			}
			a.Dir = "down"
			if a.Trigger.GreaterThan(ref) {
				a.Dir = "up"
			}
		} else {
			up := (a.Kind == "tp") == long
			if (up && !a.Trigger.GreaterThan(ref)) || (!up && !a.Trigger.LessThan(ref)) {
				return bad("INVALID_TPSL", "止盈/止损触发价方向不正确")
			}
			a.Dir = "down"
			if up {
				a.Dir = "up"
			}
		}
		out, err = scanAlgo(tx.QueryRowContext(ctx, `UPDATE fut_algo_orders SET trigger_price=$2, trigger_type=$3, trigger_dir=$4, order_type=$5, price=$6, qty=$7, updated_at=now()
			WHERE id=$1 RETURNING `+algoCols, a.ID, a.Trigger, a.TriggerType, a.Dir, a.Type, a.Price, a.Qty))
		return err
	})
	return out, err
}

func (s *Server) amendAlgo(kinds ...string) userHandler {
	return func(w http.ResponseWriter, r *http.Request, uid string) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, errNotFound)
			return
		}
		var req AlgoAmendReq
		if !decode(w, r, &req) {
			return
		}
		a, err := s.AmendAlgo(r.Context(), uid, id, kinds, req)
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"order": a})
	}
}
