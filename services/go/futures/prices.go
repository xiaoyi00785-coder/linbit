package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/shopspring/decimal"
)

// prices.go: index price (spot last price from market-data, fail-closed), mark
// price, premium sampling and 8-hourly funding settlement.

type priceState struct {
	Index    decimal.Decimal
	IndexAt  time.Time
	Mark     decimal.Decimal
	Last     decimal.Decimal
	LastRate decimal.Decimal // last settled funding rate (mark basis)
	PremSum  decimal.Decimal
	PremN    int64
	Bucket   int64 // funding interval index already settled / started
}

func (s *Server) bucket(t time.Time) int64 {
	sec := int64(s.cfg.FundingInterval / time.Second)
	if sec <= 0 {
		sec = 8 * 3600
	}
	return t.Unix() / sec
}

func (s *Server) nextFunding(t time.Time) time.Time {
	sec := int64(s.cfg.FundingInterval / time.Second)
	return time.Unix((s.bucket(t)+1)*sec, 0).UTC()
}

// mark returns the current mark price; ok=false when the index is unavailable.
func (s *Server) mark(sym string) (decimal.Decimal, bool) {
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	ps := s.prices[sym]
	if ps == nil || ps.Mark.Sign() <= 0 {
		return dZero, false
	}
	return ps.Mark, true
}

func (s *Server) setLast(sym string, px decimal.Decimal) {
	s.pmu.Lock()
	if ps := s.prices[sym]; ps != nil {
		ps.Last = px
	}
	s.pmu.Unlock()
}

func (s *Server) snapshot(sym string) priceState {
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	if ps := s.prices[sym]; ps != nil {
		return *ps
	}
	return priceState{}
}

func (s *Server) predictedRate(ps priceState) decimal.Decimal {
	avg := dZero
	if ps.PremN > 0 {
		avg = ps.PremSum.Div(decimal.NewFromInt(ps.PremN))
	}
	return fundingRate(avg, s.cfg.FundingInterest, s.cfg.FundingClamp, s.cfg.FundingCap)
}

// refreshPrices fetches the index for every contract and recomputes marks.
func (s *Server) refreshPrices(ctx context.Context) {
	now := s.now()
	idx := map[string]decimal.Decimal{}
	for _, sym := range s.symbols {
		s.pmu.RLock()
		dev, isDev := s.devIndex[sym]
		s.pmu.RUnlock()
		if isDev {
			idx[sym] = dev
			continue
		}
		if p, ok := s.market.LastPrice(ctx, s.contracts[sym].Base); ok && p.Sign() > 0 {
			idx[sym] = p
		}
	}
	frac := decimal.NewFromInt(int64(s.nextFunding(now).Sub(now) / time.Second)).Div(decimal.NewFromInt(int64(s.cfg.FundingInterval / time.Second)))
	s.pmu.Lock()
	defer s.pmu.Unlock()
	for _, sym := range s.symbols {
		ps := s.prices[sym]
		if p, ok := idx[sym]; ok {
			ps.Index, ps.IndexAt = p, now
		} else if now.Sub(ps.IndexAt) > s.cfg.IndexStale {
			ps.Index = dZero // fail closed: no mark, no new orders, no liquidations
		}
		if ps.Index.Sign() <= 0 {
			ps.Mark = dZero
			continue
		}
		c := s.contracts[sym]
		ps.Mark = markPrice(ps.Index, ps.Last, ps.LastRate, frac).Div(c.Tick).Round(0).Mul(c.Tick)
		prem := dZero
		if ps.Last.Sign() > 0 {
			prem = ps.Last.Sub(ps.Index).Div(ps.Index)
		}
		ps.PremSum = ps.PremSum.Add(prem)
		ps.PremN++
	}
}

// sampleMarks writes the mark-price history at MarkSampleEvery resolution.
func (s *Server) sampleMarks(ctx context.Context, force bool) {
	now := s.now().UTC().Truncate(time.Second)
	if !force && now.Sub(s.lastMarkSample) < s.cfg.MarkSampleEvery {
		return
	}
	s.lastMarkSample = now
	for _, sym := range s.symbols {
		ps := s.snapshot(sym)
		if ps.Mark.Sign() <= 0 {
			continue
		}
		_, _ = s.db.ExecContext(ctx, `INSERT INTO fut_mark_history (symbol, ts, index_price, mark_price) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, sym, now, ps.Index, ps.Mark)
	}
}

// fundingDue settles every contract whose funding boundary has passed.
// Caller holds s.mu.
func (s *Server) fundingDue(ctx context.Context) {
	b := s.bucket(s.now())
	for _, sym := range s.symbols {
		ps := s.snapshot(sym)
		if b <= ps.Bucket {
			continue
		}
		at := time.Unix(b*int64(s.cfg.FundingInterval/time.Second), 0).UTC()
		if ps.Mark.Sign() <= 0 {
			log.Printf("futures: funding %s skipped at %s: no mark price", sym, at)
		} else if err := s.settleFunding(ctx, sym, s.predictedRate(ps), ps.Mark, at); err != nil {
			log.Printf("futures: funding %s: %v", sym, err)
			continue
		}
		s.pmu.Lock()
		s.prices[sym].Bucket = b
		s.pmu.Unlock()
	}
}

// settleFunding moves q*mark*rate from longs to shorts (rate>0) or back.
// Idempotent per (symbol, funding_time). Caller holds s.mu.
func (s *Server) settleFunding(ctx context.Context, sym string, rate, mark decimal.Decimal, at time.Time) error {
	err := s.txLocked(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO fut_funding_history (symbol, funding_time, rate, mark_price) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, sym, at, rate, mark)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+posCols+` FROM fut_positions WHERE symbol=$1 AND qty <> 0 FOR UPDATE`, sym)
		if err != nil {
			return err
		}
		var ps []Position
		for rows.Next() {
			p, err := scanPos(rows)
			if err != nil {
				rows.Close()
				return err
			}
			ps = append(ps, p)
		}
		rows.Close()
		ref := "funding:" + sym + ":" + at.Format(time.RFC3339)
		for _, p := range ps {
			pay := money(p.Qty.Mul(mark).Mul(rate)) // >0: this position pays
			if pay.IsZero() {
				continue
			}
			if p.Mode == "isolated" {
				p.Iso = p.Iso.Sub(pay)
				if err := savePosition(ctx, tx, p); err != nil {
					return err
				}
				if err := addBill(ctx, tx, p.UserID, sym, "funding", pay.Neg(), ref); err != nil {
					return err
				}
			} else if err := addBalance(ctx, tx, p.UserID, sym, "funding", pay.Neg(), ref); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.pmu.Lock()
	if ps := s.prices[sym]; ps != nil {
		ps.LastRate, ps.PremSum, ps.PremN = rate, dZero, 0
	}
	s.pmu.Unlock()
	return nil
}
