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

// AmendReq changes price and/or qty of an open limit order. Empty = unchanged.
// qty is the new total order quantity and must stay above the filled amount.
type AmendReq struct {
	Price string `json:"price"`
	Qty   string `json:"qty"`
}

// losesPriority: a new price or a larger quantity re-queues the order (new seq);
// reducing quantity at the same price keeps its time priority.
func losesPriority(oldPx, newPx, oldQty, newQty decimal.Decimal) bool {
	return !oldPx.Equal(newPx) || newQty.GreaterThan(oldQty)
}

func (s *Server) AmendOrder(ctx context.Context, uid string, id int64, r AmendReq) (Order, []Trade, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, t, err := s.amendLocked(ctx, uid, id, r)
	s.processCopy(ctx)
	return o, t, err
}

func (s *Server) amendLocked(ctx context.Context, uid string, id int64, r AmendReq) (Order, []Trade, error) {
	var o Order
	var trades []Trade
	var c Contract
	err := s.txLocked(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		var err error
		o, err = scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderCols+` FROM fut_orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, uid))
		if err == sql.ErrNoRows {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if !o.open() {
			return bad("ORDER_NOT_OPEN", "订单已完成或已撤销")
		}
		if o.Type != "limit" || !o.Price.Valid {
			return bad("AMEND_NOT_ALLOWED", "仅限价挂单可修改")
		}
		if o.Source != "user" {
			return bad("AMEND_NOT_ALLOWED", "系统/跟单/触发生成的订单不能修改")
		}
		if c, err = s.contract(o.Symbol); err != nil {
			return err
		}
		if c.Status != "trading" {
			return errContractHalted
		}
		cfg, err := loadOrderConfig(ctx, tx)
		if err != nil {
			return err
		}
		if o.AmendCount >= cfg.MaxAmendsPerOrder {
			return bad("AMEND_LIMIT", fmt.Sprintf("每笔订单最多修改 %d 次", cfg.MaxAmendsPerOrder))
		}
		px, qty := o.Price.Decimal, o.Qty
		if v := strings.TrimSpace(r.Price); v != "" {
			if px, err = decimal.NewFromString(v); err != nil || px.Sign() <= 0 || !isStep(px, c.Tick) {
				return bad("INVALID_PRICE", fmt.Sprintf("价格须为 %s 的整数倍", c.Tick))
			}
		}
		if v := strings.TrimSpace(r.Qty); v != "" {
			if qty, err = decimal.NewFromString(v); err != nil || qty.Sign() <= 0 || !isStep(qty, c.MinQty) {
				return bad("INVALID_QTY", fmt.Sprintf("数量须为 %s 的整数倍", c.MinQty))
			}
		}
		if px.Equal(o.Price.Decimal) && qty.Equal(o.Qty) {
			return bad("NO_CHANGE", "价格和数量均未变化")
		}
		if !qty.GreaterThan(o.Filled) {
			return bad("INVALID_QTY", "修改后的数量必须大于已成交数量")
		}
		if qty.GreaterThan(c.MaxQty) {
			return bad("INVALID_QTY", "超过单笔最大数量")
		}
		if (o.TP.Valid && (o.Side == "buy") != o.TP.Decimal.GreaterThan(px)) || (o.SL.Valid && (o.Side == "buy") != o.SL.Decimal.LessThan(px)) {
			return bad("INVALID_TPSL", "修改后价格与附带的止盈/止损方向冲突")
		}
		mark, ok := s.mark(c.Symbol)
		if !ok {
			return errMarkUnavail
		}
		cross, err := wouldTake(ctx, tx, c.Symbol, o.Side, px)
		if err != nil {
			return err
		}
		if cross && o.TIF == "POST_ONLY" {
			return bad("POST_ONLY_REJECTED", "只做 Maker 订单修改后会立即成交，已拒绝")
		}
		rem := qty.Sub(o.Filled)
		reserve := dZero
		if !o.ReduceOnly {
			reserve = money(rem.Mul(px).Div(decimal.NewFromInt(int64(o.Leverage))).Add(rem.Mul(px).Mul(s.reserveTaker(uid, c))))
			if delta := reserve.Sub(o.Reserved); delta.Sign() > 0 {
				st, err := s.state(ctx, tx, uid)
				if err != nil {
					return err
				}
				if st.Available.LessThan(delta) {
					return errInsufficient
				}
			}
			if qty.GreaterThan(o.Qty) {
				p, _, err := loadPosition(ctx, tx, uid, c.Symbol, o.PosSide, false)
				if err != nil {
					return err
				}
				var pend decimal.Decimal
				if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(qty-filled),0) FROM fut_orders WHERE user_id=$1 AND symbol=$2 AND side=$3 AND position_side=$4 AND NOT reduce_only AND status IN ('new','partially_filled') AND id<>$5`,
					uid, c.Symbol, o.Side, o.PosSide, o.ID).Scan(&pend); err != nil {
					return err
				}
				if lim := c.maxNotional(o.Leverage); p.Qty.Add(signed(o.Side, rem.Add(pend))).Abs().Mul(px).GreaterThan(lim) {
					return bad("POSITION_LIMIT", fmt.Sprintf("超过 %dx 杠杆可持有的最大仓位价值 %s USDT", o.Leverage, lim))
				}
			}
		}
		if err := addFrozen(ctx, tx, uid, reserve.Sub(o.Reserved)); err != nil {
			return err
		}
		seq := "seq"
		if losesPriority(o.Price.Decimal, px, o.Qty, qty) {
			seq = "nextval('fut_orders_seq')"
		}
		if o, err = scanOrder(tx.QueryRowContext(ctx, `UPDATE fut_orders SET price=$2, qty=$3, reserved=$4, amend_count=amend_count+1, seq=`+seq+`, updated_at=now() WHERE id=$1 RETURNING `+orderCols,
			o.ID, px, qty, reserve)); err != nil {
			return err
		}
		if cross {
			trades, err = s.match(ctx, tx, c, &o, mark)
		}
		return err
	})
	if err != nil {
		return Order{}, nil, err
	}
	if len(trades) > 0 {
		s.setLast(c.Symbol, trades[len(trades)-1].Price)
	}
	return o, trades, nil
}

func (s *Server) hAmend(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, errNotFound)
		return
	}
	var req AmendReq
	if !decode(w, r, &req) {
		return
	}
	o, trades, err := s.AmendOrder(r.Context(), uid, id, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	if trades == nil {
		trades = []Trade{}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": o, "trades": trades})
}
