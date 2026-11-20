package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/shopspring/decimal"
)

// RunDCA polls `dca_plans` on DCAPoll intervals and fires any plan whose
// next_run_at is <= now. It is safe to run alongside the grid poller; both
// share s.mu to serialise state changes against API calls.
func (s *Server) RunDCA(ctx context.Context) {
	t := time.NewTicker(s.cfg.DCAPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.TickDCA(ctx)
		}
	}
}

// TickDCA runs one scheduler pass: every active plan whose next_run_at is
// <= now is fired once (per tick). Each fire places a market order through
// the order service and records a dca_executions row.
func (s *Server) TickDCA(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	due, err := s.dcaPlans(ctx, `WHERE status='active' AND next_run_at <= $1 ORDER BY id`, now)
	if err != nil {
		log.Printf("dca tick: %v", err)
		return
	}
	for _, p := range due {
		if err := s.fireDCA(ctx, p, now); err != nil {
			log.Printf("dca plan %d fire: %v", p.ID, err)
		}
	}
}

// fireDCA places one market order for the plan and books it. On error the
// execution row is still recorded with status=failed so the user sees it.
func (s *Server) fireDCA(ctx context.Context, p DCAPlan, now time.Time) error {
	// Guard: end_at past.
	if p.EndAt.Valid && !now.Before(p.EndAt.Time) {
		_, err := s.db.ExecContext(ctx, `UPDATE dca_plans SET status='stopped', stopped_at=$2,
			stop_reason=COALESCE(stop_reason,'end_reached') WHERE id=$1`, p.ID, now)
		return err
	}
	// Price trigger: only fire when the trigger condition is met (buy -> price
	// at or below triggerPrice; sell -> price at or above).
	if p.TriggerType == "price" && p.TriggerPrice.Valid {
		price, err := s.priceCall(ctx, p.Symbol)
		if err != nil {
			// Market data down: skip this tick, but do NOT bump next_run_at —
			// we want to retry on the next poll.
			return err
		}
		ok := (p.Side == "buy" && !price.GreaterThan(p.TriggerPrice.Decimal)) ||
			(p.Side == "sell" && !price.LessThan(p.TriggerPrice.Decimal))
		if !ok {
			return nil
		}
	}

	// Insert the pending execution row first, so a crash mid-flight leaves a
	// trail the operator can see and the user is told the attempt happened.
	req := placeReq{Symbol: p.Symbol, Side: p.Side, Type: "market",
		ClientOrderID: fmt.Sprintf("dca-%d-%d", p.ID, now.UnixNano()%1e9)}
	var reqQuote, reqBase decimal.Decimal
	if p.Side == "buy" {
		q := p.AmountPerRun.String()
		req.QuoteOrderQty = &q
		reqQuote = p.AmountPerRun
	} else {
		q := p.AmountPerRun.String()
		req.Quantity = &q
		reqBase = p.AmountPerRun
	}
	var execID int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO dca_executions
		(plan_id,user_id,symbol,side,requested_quote,requested_base) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		p.ID, p.UserID, p.Symbol, p.Side, reqQuote, reqBase).Scan(&execID); err != nil {
		return err
	}

	// Bump next_run_at BEFORE placing the order so a transient crash while the
	// HTTP call is in flight cannot cause a double-send on restart. If the
	// order service rejects the call we still move on; the user sees the
	// failed execution and can adjust their plan.
	next := nextRun(p, now, s.cfg.DCAMaxCatchup)
	update := `UPDATE dca_plans SET next_run_at=$2, last_run_at=$3 WHERE id=$1`
	if _, err := s.db.ExecContext(ctx, update, p.ID, next, now); err != nil {
		return err
	}

	o, err := s.placeCall(ctx, p.UserID, req)
	if err != nil {
		msg := err.Error()
		_, _ = s.db.ExecContext(ctx, `UPDATE dca_executions SET status='failed', error=$2, done_at=now() WHERE id=$1`, execID, msg)
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE dca_executions SET order_id=$2 WHERE id=$1`, execID, o.OrderID); err != nil {
		return err
	}

	info, err := s.waitFinalDCA(ctx, p.UserID, o.OrderID)
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE dca_executions SET status='failed', error=$2, done_at=now() WHERE id=$1`,
			execID, err.Error())
		return err
	}

	var avg decimal.NullDecimal
	if info.FilledQuantity.Sign() > 0 {
		avg = decimal.NullDecimal{Decimal: info.FilledQuote.Div(info.FilledQuantity).Round(18), Valid: true}
	}
	status := execCanceled
	if info.Order.Status == "filled" {
		status = execFilled
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE dca_executions SET status=$2, filled_base=$3, filled_quote=$4,
		fee_base=$5, fee_quote=$6, avg_price=$7, done_at=now() WHERE id=$1`,
		execID, status, info.FilledQuantity, info.FilledQuote, info.BaseFee, info.QuoteFee, avg); err != nil {
		return err
	}
	// Aggregate totals: for buy, the user received base and spent quote; for sell,
	// they gave up base and received quote. Fees are tracked in the asset paid.
	if _, err := tx.ExecContext(ctx, `UPDATE dca_plans SET total_runs=total_runs+1,
		total_base=total_base+$2, total_quote=total_quote+$3, total_fee_base=total_fee_base+$4,
		total_fee_quote=total_fee_quote+$5 WHERE id=$1`,
		p.ID, info.FilledQuantity, info.FilledQuote, info.BaseFee, info.QuoteFee); err != nil {
		return err
	}
	return tx.Commit()
}

// nextRun advances the plan's schedule past `now` by whole intervals, capped
// at maxCatchup steps so a long outage doesn't fire a storm on restart.
func nextRun(p DCAPlan, now time.Time, maxCatchup int) time.Time {
	if maxCatchup < 1 {
		maxCatchup = 1
	}
	iv := time.Duration(p.IntervalSec) * time.Second
	next := p.NextRunAt.Add(iv)
	// If next is still in the past, jump forward by whole intervals until it
	// is in the future (never more than maxCatchup+1 steps total).
	for steps := 0; !next.After(now) && steps < maxCatchup*8; steps++ {
		next = next.Add(iv)
	}
	return next
}

// placeCall / orderInfoCall / priceCall route through the test hooks when set,
// otherwise through the real HTTP client.
func (s *Server) placeCall(ctx context.Context, uid string, r placeReq) (orderDTO, error) {
	if s.placeFn != nil {
		return s.placeFn(ctx, uid, r)
	}
	return s.place(ctx, uid, r)
}

func (s *Server) orderInfoCall(ctx context.Context, uid, id string) (orderInfo, error) {
	if s.orderInfoFn != nil {
		return s.orderInfoFn(ctx, uid, id)
	}
	return s.orderInfo(ctx, uid, id)
}

func (s *Server) priceCall(ctx context.Context, sym string) (decimal.Decimal, error) {
	if s.marketPriceFn != nil {
		return s.marketPriceFn(ctx, sym)
	}
	return s.lastPrice(ctx, sym)
}

func (s *Server) symbolCall(ctx context.Context, sym string) (SymbolInfo, error) {
	if s.symbolFn != nil {
		return s.symbolFn(ctx, sym)
	}
	return s.symbol(ctx, sym)
}

// waitFinalDCA is waitFinal but it goes through orderInfoCall so tests can
// stub the status transition.
func (s *Server) waitFinalDCA(ctx context.Context, uid, id string) (orderInfo, error) {
	var last orderInfo
	var err error
	for i := 0; i < 50; i++ {
		last, err = s.orderInfoCall(ctx, uid, id)
		if err == nil && last.final() {
			return last, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err == nil {
		err = errors.New("order not final")
	}
	return last, err
}

// countExecutions is a small helper for tests and the summary endpoint.
func (s *Server) countExecutions(ctx context.Context, planID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM dca_executions WHERE plan_id=$1`, planID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
