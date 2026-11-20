package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// trailing.go: trailing-stop algo orders.
//
// A trailing stop is a reduce-only close order with a moving trigger: the engine
// tracks the favourable extreme after an optional activation price is touched,
// and fires a market close when the reference price retraces from that peak by
// callbackRate (fraction, 0.001..0.10) or callbackAmount (absolute price step).
// Exactly one of the two must be given.
//
// Fire geometry:
//   sell trailing (close LONG): peak = max(ref), fire when ref <= peak * (1 - cr)
//   buy  trailing (close SHORT): peak = min(ref), fire when ref >= peak * (1 + cr)
//
// Rows live in fut_algo_orders with kind='trailing_stop'. activation_price and
// one of callback_rate / callback_amount are set at create; peak_price and
// activated_at move under the engine lock on each tick.

type TrailingReq struct {
	Symbol          string `json:"symbol"`
	PositionSide    string `json:"positionSide"`
	Qty             string `json:"qty"` // empty = whole position (at fire time)
	ActivationPrice string `json:"activationPrice"`
	CallbackRate    string `json:"callbackRate"`   // fraction, e.g. "0.01" for 1%
	CallbackAmount  string `json:"callbackAmount"` // alternative: absolute price delta
	TriggerType     string `json:"triggerPriceType"`
}

const (
	minCallbackRate = "0.001"
	maxCallbackRate = "0.10"
)

func (s *Server) CreateTrailing(ctx context.Context, uid string, r TrailingReq) (AlgoOrder, error) {
	c, err := s.contract(r.Symbol)
	if err != nil {
		return AlgoOrder{}, err
	}
	if c.Status != "trading" {
		return AlgoOrder{}, errContractHalted
	}
	rateStr := strings.TrimSpace(r.CallbackRate)
	amtStr := strings.TrimSpace(r.CallbackAmount)
	if (rateStr == "" || rateStr == "0") == (amtStr == "" || amtStr == "0") {
		return AlgoOrder{}, bad("VALIDATION_ERROR", "callbackRate 与 callbackAmount 必须二选一")
	}
	var rate, amt decimal.NullDecimal
	if rateStr != "" && rateStr != "0" {
		d, err := decimal.NewFromString(rateStr)
		if err != nil || d.LessThan(decimal.RequireFromString(minCallbackRate)) || d.GreaterThan(decimal.RequireFromString(maxCallbackRate)) {
			return AlgoOrder{}, bad("VALIDATION_ERROR", "callbackRate 必须在 0.1% 到 10% 之间")
		}
		rate = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	if amtStr != "" && amtStr != "0" {
		d, err := decimal.NewFromString(amtStr)
		if err != nil || d.Sign() <= 0 || !isStep(d, c.Tick) {
			return AlgoOrder{}, bad("INVALID_PRICE", fmt.Sprintf("callbackAmount 须为 %s 的整数倍", c.Tick))
		}
		amt = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	var act decimal.NullDecimal
	if s := strings.TrimSpace(r.ActivationPrice); s != "" && s != "0" {
		d, err := decimal.NewFromString(s)
		if err != nil || d.Sign() <= 0 || !isStep(d, c.Tick) {
			return AlgoOrder{}, bad("INVALID_PRICE", fmt.Sprintf("activationPrice 须为 %s 的整数倍", c.Tick))
		}
		act = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	side := strings.ToUpper(strings.TrimSpace(r.PositionSide))
	if side == "" {
		if side, err = s.defaultCloseSide(ctx, uid, c.Symbol); err != nil {
			return AlgoOrder{}, err
		}
	}
	var out AlgoOrder
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockFutUser(ctx, tx, uid); err != nil {
			return err
		}
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
				return bad("INVALID_QTY", "追踪止损数量不能超过持仓数量")
			}
			qty = decimal.NullDecimal{Decimal: d, Valid: true}
		}
		tt, err := normTrigger(r.TriggerType, cfg.TPSLDefaultTrigger)
		if err != nil {
			return err
		}
		ref, ok := s.triggerRef(c.Symbol, tt)
		if !ok {
			return errMarkUnavail
		}
		// Activation must sit on the favourable side of the reference price
		// (above for a LONG close, below for a SHORT close). An omitted
		// activation price activates as soon as the row lands.
		dir := "down"
		if long {
			dir = "up"
		}
		if act.Valid {
			if (long && !act.Decimal.GreaterThan(ref)) || (!long && !act.Decimal.LessThan(ref)) {
				return bad("INVALID_TRIGGER", "activationPrice 方向不正确")
			}
		}
		if err := s.checkAlgoLimit(ctx, tx, uid, c.Symbol, 1, cfg); err != nil {
			return err
		}
		a := AlgoOrder{UserID: uid, Symbol: c.Symbol, Kind: "trailing_stop", Side: closeSide, PosSide: side,
			Type: "market", Qty: qty, Trigger: dZero, TriggerType: tt, Dir: dir, ReduceOnly: true, TIF: "GTC",
			Activation: act, CallbackRate: rate, CallbackAmount: amt}
		if !act.Valid {
			// immediate activation at the current reference price
			a.Peak = decimal.NullDecimal{Decimal: ref, Valid: true}
		}
		out, err = insertAlgo(ctx, tx, a)
		if err != nil {
			return err
		}
		if !act.Valid {
			// mark activated_at on immediate-activation rows (insertAlgo cannot
			// set it: it's not in the INSERT column list).
			_, err = tx.ExecContext(ctx, `UPDATE fut_algo_orders SET activated_at=now(), updated_at=now() WHERE id=$1`, out.ID)
			if err == nil {
				out.ActivatedAt = sql.NullTime{Time: s.now(), Valid: true}
				t := out.ActivatedAt.Time
				out.ActivatedTS = &t
			}
		}
		return err
	})
	return out, err
}

// updateTrailing moves the peak and decides whether the stop should fire on
// this tick. Caller holds s.mu. Returns the new peak, a flag for "activated on
// this tick" and a flag for "fire now".
func updateTrailing(a AlgoOrder, ref decimal.Decimal) (peak decimal.Decimal, nowActivated, fire bool) {
	long := a.Side == "sell"
	peak = ref
	if a.Peak.Valid {
		peak = a.Peak.Decimal
	}
	if !a.ActivatedAt.Valid {
		// pre-activation: wait for the reference to reach activation_price.
		if !a.Activation.Valid {
			return ref, true, false
		}
		if long {
			if ref.GreaterThanOrEqual(a.Activation.Decimal) {
				return ref, true, false
			}
			return peak, false, false
		}
		if ref.LessThanOrEqual(a.Activation.Decimal) {
			return ref, true, false
		}
		return peak, false, false
	}
	// activated: track the favourable extreme.
	if long {
		if ref.GreaterThan(peak) {
			peak = ref
		}
	} else {
		if ref.LessThan(peak) {
			peak = ref
		}
	}
	// compute the retracement trigger from the peak.
	var trig decimal.Decimal
	if a.CallbackAmount.Valid {
		if long {
			trig = peak.Sub(a.CallbackAmount.Decimal)
		} else {
			trig = peak.Add(a.CallbackAmount.Decimal)
		}
	} else {
		cr := a.CallbackRate.Decimal
		if long {
			trig = peak.Mul(dOne.Sub(cr))
		} else {
			trig = peak.Mul(dOne.Add(cr))
		}
	}
	if long {
		fire = ref.LessThanOrEqual(trig)
	} else {
		fire = ref.GreaterThanOrEqual(trig)
	}
	return peak, false, fire
}

func (s *Server) hCreateTrailing(w http.ResponseWriter, r *http.Request, uid string) {
	var req TrailingReq
	if !decode(w, r, &req) {
		return
	}
	a, err := s.CreateTrailing(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": a})
}
