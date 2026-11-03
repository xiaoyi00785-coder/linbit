package main

// convert_risk.go: 闪兑 risk controls.
//
//  1. USDC depeg guard: when the USDC index moves more than depegBps
//     (default 50 = 0.5%) away from 1 — or the USDC index is missing/stale —
//     USDC↔USDT convert is paused (quote and accept return
//     503 PAIR_PAUSED_DEPEG). It resumes automatically once the price is back
//     in range. Every pause/resume writes a convert_events row (audit) and
//     sends a Telegram alert.
//
//  2. Automatic hedging (convert_hedge.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

const depegPair = "USDC/USDT"

// isDepegGuarded: the pairs covered by the USDC depeg guard.
func isDepegGuarded(from, to string) bool {
	return (from == "USDC" && to == "USDT") || (from == "USDT" && to == "USDC")
}

// DepegState is the evaluated guard state.
type DepegState struct {
	Pair         string `json:"pair"`
	Paused       bool   `json:"paused"`
	Reason       string `json:"reason,omitempty"` // depeg | price_unavailable
	Price        string `json:"price,omitempty"`
	DeviationBps string `json:"deviationBps,omitempty"`
	ThresholdBps int    `json:"thresholdBps"`
	ChangedAt    int64  `json:"changedAt,omitempty"`
}

// EvalDepeg is pure (unit tested): missing/stale/invalid price = paused.
func EvalDepeg(p decimal.Decimal, at, now time.Time, err error, maxAge time.Duration, thresholdBps int) DepegState {
	st := DepegState{Pair: depegPair, ThresholdBps: thresholdBps}
	if err != nil || p.Sign() <= 0 || (!at.IsZero() && now.Sub(at) > maxAge) {
		st.Paused, st.Reason = true, "price_unavailable"
		return st
	}
	dev := p.Sub(decimal.NewFromInt(1)).Abs().Mul(decimal.NewFromInt(10000))
	st.Price, st.DeviationBps = p.String(), dev.Round(2).String()
	if dev.GreaterThan(decimal.NewFromInt(int64(thresholdBps))) {
		st.Paused, st.Reason = true, "depeg"
	}
	return st
}

// alerter is swapped in tests.
var convertAlert = common.AlertTelegram

type depegGuard struct {
	mu   sync.Mutex
	last *bool // last persisted paused flag (nil = not loaded)
}

// checkDepeg evaluates the guard now and records a transition (audit + alert).
func (s *Server) checkDepeg(ctx context.Context) DepegState {
	thr := 50
	if cfg, err := s.store.convertConfig(ctx, s.cfg.Convert); err == nil {
		thr = cfg.DepegBps
	}
	p, at, err := s.prices(ctx, "USDC")
	st := EvalDepeg(p, at, s.now(), err, s.cfg.Convert.PriceMaxAge, thr)
	s.recordDepeg(ctx, st)
	return st
}

func (s *Server) recordDepeg(ctx context.Context, st DepegState) {
	g := &s.depeg
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		var paused bool
		err := s.store.db.QueryRowContext(ctx, `SELECT paused FROM convert_pair_state WHERE pair=$1`, depegPair).Scan(&paused)
		if err != nil { // first run: unpaused baseline
			paused = false
		}
		g.last = &paused
	}
	if *g.last == st.Paused {
		return
	}
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO convert_pair_state (pair, paused, reason, price, changed_at) VALUES ($1,$2,$3,$4,now())
		ON CONFLICT (pair) DO UPDATE SET paused=EXCLUDED.paused, reason=EXCLUDED.reason, price=EXCLUDED.price, changed_at=now()`,
		depegPair, st.Paused, st.Reason, st.Price); err != nil {
		log.Printf("convert depeg state: %v", err)
		return
	}
	v := st.Paused
	g.last = &v
	kind, msg := "pair_resumed", fmt.Sprintf("闪兑恢复：USDC/USDT 价格回到范围内（USDC=%s，阈值 %d bps）", st.Price, st.ThresholdBps)
	if st.Paused {
		kind = "pair_paused"
		if st.Reason == "depeg" {
			msg = fmt.Sprintf("闪兑暂停：USDC 脱锚，USDC=%s 偏离 %s bps（阈值 %d bps），USDC/USDT 闪兑已自动暂停", st.Price, st.DeviationBps, st.ThresholdBps)
		} else {
			msg = "闪兑暂停：USDC 指数价格缺失或过期，USDC/USDT 闪兑已自动暂停"
		}
	}
	s.store.convertEvent(ctx, kind, depegPair, st)
	convertAlert(msg)
}

func (s *Store) convertEvent(ctx context.Context, kind, target string, detail any) {
	b, _ := json.Marshal(detail)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO convert_events (kind, target, detail) VALUES ($1,$2,$3)`, kind, target, string(b)); err != nil {
		log.Printf("convert event %s: %v", kind, err)
	}
}

// pairGuard returns PAIR_PAUSED_DEPEG for a guarded pair that is paused.
func (s *Server) pairGuard(ctx context.Context, from, to string) error {
	if !isDepegGuarded(from, to) {
		return nil
	}
	if st := s.checkDepeg(ctx); st.Paused {
		return cerr(http.StatusServiceUnavailable, "PAIR_PAUSED_DEPEG", "USDC 价格偏离，USDC/USDT 闪兑暂停，请稍后再试")
	}
	return nil
}

// runDepegMonitor re-evaluates the guard periodically so pause/resume is
// detected (and alerted) even without quote traffic.
func (s *Server) runDepegMonitor(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		s.checkDepeg(cctx)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// GET /internal/convert/events?kind=&page=&pageSize=
func (s *Server) handleConvertEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 1, 1, 1<<30)
	size := atoiDefault(q.Get("pageSize"), 20, 1, 100)
	var total int
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM convert_events WHERE ($1='' OR kind=$1)`, q.Get("kind")).Scan(&total); err != nil {
		writeConvertErr(w, "events", err)
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT id, kind, target, detail::text, created_at FROM convert_events WHERE ($1='' OR kind=$1)
		ORDER BY id DESC LIMIT $2 OFFSET $3`, q.Get("kind"), size, (page-1)*size)
	if err != nil {
		writeConvertErr(w, "events", err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var kind, target, detail string
		var at time.Time
		if err := rows.Scan(&id, &kind, &target, &detail, &at); err != nil {
			writeConvertErr(w, "events", err)
			return
		}
		items = append(items, map[string]any{"id": id, "kind": kind, "target": target, "detail": json.RawMessage(detail), "createdAt": at.UnixMilli()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "pageSize": size, "total": total})
}

// startConvertRisk validates the hedge account and starts the depeg monitor
// and the hedge worker.
func (s *Server) startConvertRisk(ctx context.Context) error {
	hu := s.cfg.Convert.HedgeUserID
	if hu == "" || hu == ConvertAccount || hu == mmUserID() {
		return fmt.Errorf("CONVERT_HEDGE_USER_ID=%q must be set and differ from %s and the market maker (MM_USER_ID)", hu, ConvertAccount)
	}
	if !strings.HasPrefix(hu, "system:") {
		log.Printf("WARNING: CONVERT_HEDGE_USER_ID=%q is not a system:* account; it will be counted as a user liability", hu)
	}
	if err := s.store.EnsureAccount(ctx, hu); err != nil {
		return err
	}
	if s.cfg.Convert.DepegCheck > 0 {
		go s.runDepegMonitor(ctx, s.cfg.Convert.DepegCheck)
	}
	if s.cfg.Convert.HedgeInterval > 0 {
		go s.runHedger(ctx)
	}
	return nil
}
