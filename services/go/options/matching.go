package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// matching.go: per-instrument price-time priority matching. The book itself
// lives in opt_orders; a new order scans the opposite side within the tx and
// writes fills + position deltas atomically.

type fill struct {
	Price decimal.Decimal
	Qty   decimal.Decimal
}

type restingOrder struct {
	ID       int64
	UserID   string
	Side     string
	Price    decimal.Decimal
	Qty      decimal.Decimal
	Filled   decimal.Decimal
}

// matchAndWrite walks the opposite side of the book in price-time order and
// writes fills until the taker is exhausted or the price is no longer
// acceptable. It returns the fills (for the taker's final avg_price calc).
func (s *Server) matchAndWrite(ctx context.Context, tx *sql.Tx, in *Instrument, takerID int64, takerUser, side, typ string, limit decimal.NullDecimal, qty decimal.Decimal) ([]fill, error) {
	opposite := "buy"
	if side == "buy" {
		opposite = "sell"
	}
	// Price condition against limit, if any.
	orderBy := "price ASC"
	priceCond := ""
	args := []any{in.ID, opposite}
	if typ == "limit" {
		if side == "buy" {
			priceCond = " AND price <= $3"
		} else {
			priceCond = " AND price >= $3"
		}
		args = append(args, limit.Decimal)
	}
	if opposite == "buy" {
		orderBy = "price DESC"
	}
	// Second ordering key: oldest first (FIFO).
	q := fmt.Sprintf(`
	    SELECT id, user_id, side, price, qty, filled_qty
	      FROM opt_orders
	     WHERE instrument_id=$1 AND side=$2 AND order_type='limit'
	       AND status IN ('open','partial') AND price IS NOT NULL%s
	     ORDER BY %s, id ASC
	     FOR UPDATE`, priceCond, orderBy)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var resting []restingOrder
	for rows.Next() {
		var o restingOrder
		if err := rows.Scan(&o.ID, &o.UserID, &o.Side, &o.Price, &o.Qty, &o.Filled); err != nil {
			rows.Close()
			return nil, err
		}
		resting = append(resting, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var fills []fill
	remain := qty
	for _, r := range resting {
		if remain.Sign() == 0 {
			break
		}
		avail := r.Qty.Sub(r.Filled)
		if avail.Sign() <= 0 {
			continue
		}
		if takerUser == r.UserID {
			// Self-trade prevention: cancel our own resting (would otherwise
			// let one user cross the book against themself).
			if _, err := tx.ExecContext(ctx,
				`UPDATE opt_orders SET status='cancelled', updated_at=now() WHERE id=$1`, r.ID); err != nil {
				return nil, err
			}
			continue
		}
		fillQty := decimal.Min(remain, avail)
		price := r.Price
		if err := s.writeFill(ctx, tx, in, takerID, takerUser, r, side, price, fillQty); err != nil {
			return nil, err
		}
		fills = append(fills, fill{Price: price, Qty: fillQty})
		remain = remain.Sub(fillQty)
	}
	return fills, nil
}

// writeFill: insert a trade row, update the maker order, apply the position
// deltas for both users. Fees are deducted from the premium flow (maker
// receives premium − fee on sell; taker pays premium + fee on buy).
func (s *Server) writeFill(ctx context.Context, tx *sql.Tx, in *Instrument, takerID int64, takerUser string, maker restingOrder, takerSide string, price, qty decimal.Decimal) error {
	fee := price.Mul(qty).Mul(in.ContractSize).Mul(s.cfg.FeeRate)
	// Maker's remaining fill status:
	newFilled := maker.Filled.Add(qty)
	status := "partial"
	if newFilled.Equal(maker.Qty) {
		status = "filled"
	}
	if _, err := tx.ExecContext(ctx, `
	    UPDATE opt_orders SET filled_qty=$1, status=$2, updated_at=now()
	     WHERE id=$3`, newFilled, status, maker.ID); err != nil {
		return err
	}
	// Insert the trade. taker_side is the AGGRESSIVE side.
	if _, err := tx.ExecContext(ctx, `
	    INSERT INTO opt_trades
	      (instrument_id, maker_order_id, taker_order_id, maker_user_id, taker_user_id,
	       taker_side, price, qty, maker_fee, taker_fee)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		in.ID, maker.ID, takerID, maker.UserID, takerUser, takerSide, price, qty, fee, fee); err != nil {
		return err
	}
	// Apply position deltas. BUY = +qty, SELL = -qty. Average price is
	// re-weighted on same-side additions; crossing the zero line realises PnL.
	if err := s.applyPos(ctx, tx, in, takerUser, takerSide, price, qty); err != nil {
		return err
	}
	makerSide := "buy"
	if takerSide == "buy" {
		makerSide = "sell"
	}
	if err := s.applyPos(ctx, tx, in, maker.UserID, makerSide, price, qty); err != nil {
		return err
	}

	// Premium flow through system:options. Taker pays (premium+fee), maker
	// receives (premium−fee). The taker's up-front lock already covered the
	// premium cap, so we only move the fee out and the maker's premium in.
	notional := price.Mul(qty).Mul(in.ContractSize)
	switch takerSide {
	case "buy":
		// Taker paid lock up-front; difference between lock and actual premium
		// stays in the account until refund on cancel/final-fill. Here we only
		// pay the maker (sell side) the premium less fee.
		if err := s.payMaker(ctx, maker.UserID, notional.Sub(fee), "optfill-maker-"); err != nil {
			return err
		}
	case "sell":
		// Maker was a buy: taker (seller) receives premium less fee into free
		// balance; maker's buy-side lock already covered the premium.
		if err := s.payMaker(ctx, takerUser, notional.Sub(fee), "optfill-taker-"); err != nil {
			return err
		}
	}
	return nil
}

// payMaker credits the user's spot free balance from system:options.
func (s *Server) payMaker(ctx context.Context, userID string, amt decimal.Decimal, refPrefix string) error {
	if amt.Sign() <= 0 {
		return nil
	}
	return s.asset.System(ctx, userID, "credit", amt, newRef(refPrefix))
}

// applyPos updates (user, instrument) net position after a fill.
func (s *Server) applyPos(ctx context.Context, tx *sql.Tx, in *Instrument, userID, side string, price, qty decimal.Decimal) error {
	var cur, avg, margin, realized decimal.Decimal
	err := tx.QueryRowContext(ctx, `
	    SELECT qty, avg_price, margin, realized_pnl
	      FROM opt_positions WHERE user_id=$1 AND instrument_id=$2 FOR UPDATE`,
		userID, in.ID).Scan(&cur, &avg, &margin, &realized)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	// Signed delta.
	d := qty
	if side == "sell" {
		d = qty.Neg()
	}
	next := cur.Add(d)
	// Same-sign addition: weighted average. Crossing zero: realise PnL on the
	// closed portion at the entry avg; new remainder starts at the fill price.
	switch {
	case cur.Sign() == 0:
		avg = price
	case sign(cur) == sign(d):
		// Adding to the side: VWAP the entry price.
		absCur := cur.Abs()
		absD := d.Abs()
		avg = cur.Abs().Mul(avg).Add(absD.Mul(price)).Div(absCur.Add(absD))
	default:
		// Reducing or flipping.
		closedQty := decimal.Min(cur.Abs(), d.Abs())
		// PnL realised for the closed portion.
		var rp decimal.Decimal
		if cur.Sign() > 0 {
			// Long being reduced by a sell at `price`.
			rp = price.Sub(avg).Mul(closedQty).Mul(in.ContractSize)
		} else {
			// Short being reduced by a buy at `price`.
			rp = avg.Sub(price).Mul(closedQty).Mul(in.ContractSize)
		}
		realized = realized.Add(rp)
		if next.Sign() == 0 {
			avg = decimal.Zero
		} else if sign(next) != sign(cur) {
			// Flipped side: new average = fill price for the remainder.
			avg = price
		}
		// Same sign as before, just smaller: avg stays.
	}

	// Margin: for a short position hold strike/underlying-based SPAN; for a
	// long position no margin, premium was paid up-front.
	if next.Sign() < 0 {
		spot, ok := s.underlyings[strings.ToUpper(in.Underlying)]
		if !ok {
			spot = in.Strike
		}
		per := decimal.NewFromFloat(InitialMarginShort(
			mustFloat(spot), mustFloat(in.Strike), mustFloat(price), in.OptionType == "call"))
		margin = per.Mul(next.Abs()).Mul(in.ContractSize)
	} else {
		margin = decimal.Zero
	}

	_, err = tx.ExecContext(ctx, `
	    INSERT INTO opt_positions (user_id, instrument_id, qty, avg_price, realized_pnl, margin, updated_at)
	    VALUES ($1,$2,$3,$4,$5,$6, now())
	    ON CONFLICT (user_id, instrument_id) DO UPDATE SET
	      qty=EXCLUDED.qty, avg_price=EXCLUDED.avg_price,
	      realized_pnl=EXCLUDED.realized_pnl, margin=EXCLUDED.margin,
	      updated_at=now()`,
		userID, in.ID, next, avg, realized, margin)
	return err
}

func sign(d decimal.Decimal) int {
	if d.Sign() > 0 {
		return 1
	}
	if d.Sign() < 0 {
		return -1
	}
	return 0
}

// expireInstruments scans instruments whose expiry has passed, snapshots the
// underlying, cash-settles every open position, cancels resting orders.
func (s *Server) expireInstruments(ctx context.Context) {
	s.mu.Lock()
	var due []*Instrument
	for _, in := range s.instruments {
		if in.Status == "active" && !s.now().Before(in.Expiry.Add(s.cfg.SettleGrace)) {
			due = append(due, in)
		}
	}
	s.mu.Unlock()
	for _, in := range due {
		if err := s.settle(ctx, in); err != nil {
			// Log and continue; next tick retries.
			fmt.Printf("options: settle %s: %v\n", in.Name, err)
		}
	}
}

// settle fixes a settlement price, computes payouts and marks instrument
// expired. Payouts land in opt_settlements and are paid out by RunPayouts.
func (s *Server) settle(ctx context.Context, in *Instrument) error {
	price, ok := s.market.IndexPrice(ctx, in.Underlying)
	if !ok {
		return fmt.Errorf("no index price")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Guard against a double-settlement if two ticks race.
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM opt_instruments WHERE id=$1 FOR UPDATE`, in.ID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return nil
	}

	// Cancel every resting order.
	if _, err := tx.ExecContext(ctx, `
	    UPDATE opt_orders SET status='cancelled', updated_at=now()
	     WHERE instrument_id=$1 AND status IN ('open','partial')`, in.ID); err != nil {
		return err
	}

	// Payout = intrinsic value at settlement price * signed qty * contract size.
	// Long ITM: positive payout to user; short ITM: negative (we owe writer's
	// side the loss).  We write opt_settlements rows; RunPayouts actually
	// moves USDT.
	rows, err := tx.QueryContext(ctx, `
	    SELECT user_id, qty FROM opt_positions WHERE instrument_id=$1 AND qty <> 0 AND NOT settled`, in.ID)
	if err != nil {
		return err
	}
	type pos struct{ user string; qty decimal.Decimal }
	var open []pos
	for rows.Next() {
		var p pos
		if err := rows.Scan(&p.user, &p.qty); err != nil {
			rows.Close()
			return err
		}
		open = append(open, p)
	}
	rows.Close()

	for _, p := range open {
		intrinsic := settlementIntrinsic(in, price)
		payout := intrinsic.Mul(p.qty).Mul(in.ContractSize) // signed
		if _, err := tx.ExecContext(ctx, `
		    INSERT INTO opt_settlements (instrument_id, user_id, qty, settle_price, payout, status)
		    VALUES ($1,$2,$3,$4,$5,'pending')
		    ON CONFLICT (instrument_id, user_id) DO NOTHING`,
			in.ID, p.user, p.qty, price, payout); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
	    UPDATE opt_positions SET settled=true, updated_at=now()
	     WHERE instrument_id=$1`, in.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
	    UPDATE opt_instruments SET status='expired', settlement_price=$1 WHERE id=$2`,
		price, in.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Reflect in memory.
	in.Status = "expired"
	in.SettlementPrice = decimal.NullDecimal{Decimal: price, Valid: true}
	return nil
}

// settlementIntrinsic = max(S - K, 0) for calls, max(K - S, 0) for puts.
func settlementIntrinsic(in *Instrument, spot decimal.Decimal) decimal.Decimal {
	if in.OptionType == "call" {
		if spot.GreaterThan(in.Strike) {
			return spot.Sub(in.Strike)
		}
		return decimal.Zero
	}
	if in.Strike.GreaterThan(spot) {
		return in.Strike.Sub(spot)
	}
	return decimal.Zero
}

// RunPayouts drives pending settlements to paid, honouring the asset
// ledger's idempotency keys.
func (s *Server) RunPayouts(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
		rows, err := s.db.QueryContext(ctx, `
		    SELECT id, user_id, payout FROM opt_settlements
		     WHERE status='pending' ORDER BY id ASC LIMIT 100`)
		if err != nil {
			continue
		}
		type pending struct {
			ID     int64
			User   string
			Payout decimal.Decimal
		}
		var batch []pending
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.ID, &p.User, &p.Payout); err != nil {
				break
			}
			batch = append(batch, p)
		}
		rows.Close()
		for _, p := range batch {
			ref := fmt.Sprintf("optset-%d", p.ID)
			dir := "credit"
			amt := p.Payout
			if amt.Sign() < 0 {
				dir = "debit"
				amt = amt.Neg()
			}
			if amt.Sign() == 0 {
				// Mark paid with no money movement.
				_, _ = s.db.ExecContext(ctx,
					`UPDATE opt_settlements SET status='paid', paid_at=now() WHERE id=$1`, p.ID)
				continue
			}
			if err := s.asset.System(ctx, p.User, dir, amt, ref); err != nil {
				_, _ = s.db.ExecContext(ctx,
					`UPDATE opt_settlements SET error=$1 WHERE id=$2`, err.Error(), p.ID)
				continue
			}
			_, _ = s.db.ExecContext(ctx,
				`UPDATE opt_settlements SET status='paid', paid_at=now(), error=NULL WHERE id=$1`, p.ID)
		}
	}
}
