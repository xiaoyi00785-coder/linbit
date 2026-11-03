package main

import (
	"context"
	"database/sql"
	"log"
	"sort"

	"github.com/shopspring/decimal"
)

// adl.go: auto-deleveraging. When a liquidated position is bankrupt (equity < 0
// at mark) and the insurance fund cannot absorb the deficit, the part the fund
// cannot cover is shifted onto the most profitable, most leveraged opposing
// positions: the bankrupt position is closed against them at the bankruptcy
// price instead of being taken over by the fund at mark.

// adlScore ranks a position for deleveraging: ROE*leverage when in profit,
// ROE/leverage when losing (higher = deleveraged first).
func adlScore(q, e, mark decimal.Decimal, lev int) decimal.Decimal {
	if q.IsZero() || e.Sign() <= 0 {
		return dZero
	}
	l := decimal.NewFromInt(int64(max(lev, 1)))
	roe := upnl(q, e, mark).Div(q.Abs().Mul(e).Div(l))
	if roe.Sign() > 0 {
		return roe.Mul(l)
	}
	return roe.Div(l)
}

// insuranceAvailable is what the fund can pay towards a shortfall now.
func insuranceAvailable(ctx context.Context, tx queryer) decimal.Decimal {
	var b decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM fut_accounts WHERE user_id=$1`, insuranceUser).Scan(&b)
	return dmax(dZero, b)
}

// bankruptPrice shifts mark by the uncovered part of a shortfall so that closing
// the position there leaves the remaining (fundable) deficit: long closes higher,
// short lower.
func bankruptPrice(q, mark, uncovered decimal.Decimal) decimal.Decimal {
	if q.IsZero() || uncovered.Sign() <= 0 {
		return mark
	}
	shift := uncovered.Div(q.Abs())
	if q.Sign() > 0 {
		return mark.Add(shift).Round(8)
	}
	return mark.Sub(shift).Round(8)
}

type adlCand struct {
	p     Position
	score decimal.Decimal
}

func (s *Server) adlCandidates(ctx context.Context, q queryer, sym string, sign int, exclude string, mark decimal.Decimal, lock bool) ([]adlCand, error) {
	cmp := "qty > 0"
	if sign < 0 {
		cmp = "qty < 0"
	}
	qs := `SELECT ` + posCols + ` FROM fut_positions WHERE symbol=$1 AND ` + cmp + ` AND user_id <> $2 AND user_id NOT LIKE 'system:%'`
	if lock {
		qs += ` ORDER BY user_id FOR UPDATE`
	}
	rows, err := q.QueryContext(ctx, qs, sym, exclude)
	if err != nil {
		return nil, err
	}
	var out []adlCand
	for rows.Next() {
		p, err := scanPos(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, adlCand{p, adlScore(p.Qty, p.Entry, mark, p.Leverage)})
	}
	rows.Close()
	sort.SliceStable(out, func(i, j int) bool { return out[i].score.GreaterThan(out[j].score) })
	return out, nil
}

// deleverage closes |p.Qty| of uid's position at px against ranked opposing
// positions and returns the quantity it could not place (taken over by the fund).
func (s *Server) deleverage(ctx context.Context, tx *sql.Tx, uid string, c Contract, p Position, px, mark decimal.Decimal) (decimal.Decimal, error) {
	rem := p.Qty.Abs()
	cands, err := s.adlCandidates(ctx, tx, c.Symbol, -p.Qty.Sign(), uid, mark, true)
	if err != nil {
		return rem, err
	}
	for _, cd := range cands {
		if rem.Sign() <= 0 {
			break
		}
		x := dmin(rem, cd.p.Qty.Abs())
		s.histStatus = "liquidated"
		if _, _, _, err := s.fill(ctx, tx, c, uid, p.Side, decimal.NewFromInt(int64(-p.Qty.Sign())).Mul(x), px, dZero, p.Leverage, p.Mode, decimal.NullDecimal{}, decimal.NullDecimal{}, "", ""); err != nil {
			return rem, err
		}
		s.histStatus = "adl"
		_, cpnl, _, err := s.fill(ctx, tx, c, cd.p.UserID, cd.p.Side, decimal.NewFromInt(int64(p.Qty.Sign())).Mul(x), px, dZero, cd.p.Leverage, cd.p.Mode, decimal.NullDecimal{}, decimal.NullDecimal{}, "", "")
		s.histStatus = "liquidated"
		if err != nil {
			return rem, err
		}
		if err := cancelReduceOnly(ctx, tx, cd.p.UserID, c.Symbol, cd.p.Side); err != nil {
			return rem, err
		}
		side := "buy"
		if cd.p.Qty.Sign() > 0 {
			side = "sell"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fut_orders (user_id, symbol, side, type, price, qty, filled, filled_quote, status, reduce_only, leverage, margin_mode, source, position_side)
			VALUES ($1,$2,$3,'market',$4,$5,$5,$6,'filled',true,$7,$8,'adl',$9)`, cd.p.UserID, c.Symbol, side, px, x, x.Mul(px), cd.p.Leverage, cd.p.Mode, cd.p.Side); err != nil {
			return rem, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fut_adl (symbol, liq_user, counter_user, qty, price, counter_pnl) VALUES ($1,$2,$3,$4,$5,$6)`,
			c.Symbol, uid, cd.p.UserID, decimal.NewFromInt(int64(p.Qty.Sign())).Mul(x), px, cpnl); err != nil {
			return rem, err
		}
		log.Printf("futures: ADL %s %s qty=%s px=%s against %s", uid, c.Symbol, x, px, cd.p.UserID)
		rem = rem.Sub(x)
	}
	return rem, nil
}

func cancelReduceOnly(ctx context.Context, tx *sql.Tx, uid, sym, side string) error {
	p, _, err := loadPosition(ctx, tx, uid, sym, side, false)
	if err != nil {
		return err
	}
	q := `SELECT ` + orderCols + ` FROM fut_orders WHERE user_id=$1 AND symbol=$2 AND position_side=$3 AND reduce_only AND status IN ('new','partially_filled')`
	if !p.Qty.IsZero() {
		return nil // still has a position: remaining reduce-only orders stay valid (matching caps them)
	}
	rows, err := tx.QueryContext(ctx, q+` FOR UPDATE`, uid, sym, normSide(side))
	if err != nil {
		return err
	}
	var list []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			rows.Close()
			return err
		}
		list = append(list, o)
	}
	rows.Close()
	for i := range list {
		if err := releaseOrder(ctx, tx, &list[i], "adl"); err != nil {
			return err
		}
	}
	return nil
}

// adlRanks fills the 1..5 deleveraging-queue indicator (5 = first in line)
// of each position, by score percentile among same-side positions.
func (s *Server) adlRanks(ctx context.Context, vs []PosView) {
	for i := range vs {
		v := &vs[i]
		if v.markMissing {
			continue
		}
		cands, err := s.adlCandidates(ctx, s.db, v.pos.Symbol, v.pos.Qty.Sign(), "", v.mark, false)
		if err != nil || len(cands) == 0 {
			continue
		}
		mine := adlScore(v.pos.Qty, v.pos.Entry, v.mark, v.pos.Leverage)
		below := 0
		for _, cd := range cands {
			if cd.score.LessThan(mine) || (cd.score.Equal(mine) && cd.p.UserID == v.pos.UserID) {
				below++
			}
		}
		v.ADLRank = max(1, (below*5+len(cands)-1)/len(cands))
	}
}
