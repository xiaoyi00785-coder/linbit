package main

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// worker.go: 1-second tick. Refresh underlying prices, recompute
// Black-Scholes mark + Greeks for every active instrument, down-sample
// history, settle anything whose expiry has passed.

// RunTicker runs forever until ctx is cancelled.
func (s *Server) RunTicker(ctx context.Context) {
	if s.cfg.TickInterval <= 0 {
		return
	}
	t := time.NewTicker(s.cfg.TickInterval)
	defer t.Stop()
	lastHist := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.refreshUnderlyings(ctx)
			s.recomputeMarks(ctx)
			if now.Sub(lastHist) >= s.cfg.MarkSampleEvery {
				s.sampleMarkHistory(ctx)
				lastHist = now
			}
			s.expireInstruments(ctx)
		}
	}
}

// refreshUnderlyings fetches the index price for each underlying referenced
// by an active instrument.
func (s *Server) refreshUnderlyings(ctx context.Context) {
	seen := map[string]bool{}
	s.mu.Lock()
	for _, in := range s.instruments {
		if in.Status == "active" {
			seen[strings.ToUpper(in.Underlying)] = true
		}
	}
	s.mu.Unlock()
	for asset := range seen {
		if p, ok := s.market.IndexPrice(ctx, asset); ok {
			s.setUnderlying(asset, p)
		}
	}
}

// recomputeMarks prices every active instrument from Black-Scholes using the
// book's mid as IV input when possible, else the config default IV.
func (s *Server) recomputeMarks(ctx context.Context) {
	s.mu.Lock()
	instrs := make([]*Instrument, 0, len(s.instruments))
	for _, in := range s.instruments {
		if in.Status == "active" {
			instrs = append(instrs, in)
		}
	}
	s.mu.Unlock()

	r := mustFloat(s.cfg.RiskFreeRate)
	defaultIV := mustFloat(s.cfg.DefaultIV)
	now := s.now()
	for _, in := range instrs {
		spot, ok := s.underlyingPrice(in.Underlying)
		if !ok {
			continue
		}
		ttYears := in.Expiry.Sub(now).Seconds() / (365.25 * 24 * 3600)
		if ttYears <= 0 {
			continue
		}
		S := mustFloat(spot)
		K := mustFloat(in.Strike)
		isCall := in.OptionType == "call"

		// IV source: the book mid, when present; otherwise defaultIV.
		iv := defaultIV
		if mid, ok := s.midPrice(ctx, in.ID); ok {
			if v, ok := BSImpliedVol(mustFloat(mid), S, K, ttYears, r, isCall); ok && v > 0 {
				iv = v
			}
		}
		price := BSPrice(S, K, ttYears, r, iv, isCall)
		if math.IsNaN(price) || price < 0 {
			continue
		}
		g := BSGreeks(S, K, ttYears, r, iv, isCall)

		m := MarkSnapshot{
			Mark:       decimal.NewFromFloat(price).Round(6),
			Underlying: spot,
			IV:         decimal.NewFromFloat(iv).Round(6),
			Delta:      decimal.NewFromFloat(g.Delta).Round(6),
			Gamma:      decimal.NewFromFloat(g.Gamma).Round(10),
			Theta:      decimal.NewFromFloat(g.Theta).Round(10),
			Vega:       decimal.NewFromFloat(g.Vega).Round(10),
			At:         now,
		}
		s.setMark(in.ID, m)

		// Upsert into opt_marks (tx-free; single row, cheap conflict).
		_, _ = s.db.ExecContext(ctx, `
		    INSERT INTO opt_marks (instrument_id, mark_price, underlying, iv, delta, gamma, theta, vega, ts)
		    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		    ON CONFLICT (instrument_id) DO UPDATE SET
		      mark_price=EXCLUDED.mark_price, underlying=EXCLUDED.underlying,
		      iv=EXCLUDED.iv, delta=EXCLUDED.delta, gamma=EXCLUDED.gamma,
		      theta=EXCLUDED.theta, vega=EXCLUDED.vega, ts=EXCLUDED.ts`,
			in.ID, m.Mark, m.Underlying, m.IV, m.Delta, m.Gamma, m.Theta, m.Vega, m.At)
	}
}

// midPrice returns (best_bid + best_ask) / 2 when both sides have liquidity.
func (s *Server) midPrice(ctx context.Context, id int64) (decimal.Decimal, bool) {
	bid, okB := s.bestBid(ctx, id)
	ask, okA := s.bestAsk(ctx, id)
	if okB && okA {
		return bid.Add(ask).Div(decimal.NewFromInt(2)), true
	}
	return decimal.Zero, false
}

// sampleMarkHistory down-samples opt_marks into opt_marks_history for charts.
func (s *Server) sampleMarkHistory(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `
	    INSERT INTO opt_marks_history (instrument_id, mark_price, underlying, iv, ts)
	    SELECT instrument_id, mark_price, underlying, iv, ts FROM opt_marks`)
}
