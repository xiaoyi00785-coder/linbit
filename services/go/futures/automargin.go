package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// automargin.go: per-(user,symbol) auto-margin. When the maintenance-margin
// ratio of an ISOLATED position falls below trigger, the engine tops up its
// iso margin from the futures wallet so the margin ratio comes back above
// trigger + a small cushion. The top-up stops at auto_margin_max for the life
// of the position; closing or flipping the position resets the counter.
//
// The worker runs every cfg.AutoMarginEvery on fut_settings rows that have
// auto_margin_enabled = true and acts on open isolated positions only. Cross
// positions ignore this setting: they already share the wallet directly.

type AutoMarginReq struct {
	Enabled    bool   `json:"enabled"`
	MaxAmount  string `json:"maxAmount"`     // total auto-margin cap over the life of the position
	TriggerMMR string `json:"triggerRatio"`  // margin ratio that fires a top-up (default 0.15)
	PosSide    string `json:"positionSide"`  // BOTH | LONG | SHORT (hedge mode)
}

type AutoMarginView struct {
	Symbol       string          `json:"symbol"`
	Enabled      bool            `json:"enabled"`
	MaxAmount    decimal.Decimal `json:"maxAmount"`
	UsedAmount   decimal.Decimal `json:"usedAmount"`
	TriggerRatio decimal.Decimal `json:"triggerRatio"`
}

func (s *Server) SetAutoMargin(ctx context.Context, uid, sym string, r AutoMarginReq) (AutoMarginView, error) {
	c, err := s.contract(sym)
	if err != nil {
		return AutoMarginView{}, err
	}
	var maxAmt decimal.Decimal
	if a := strings.TrimSpace(r.MaxAmount); a != "" {
		d, err := decimal.NewFromString(a)
		if err != nil || d.Sign() < 0 {
			return AutoMarginView{}, bad("INVALID_AMOUNT", "maxAmount 不合法")
		}
		maxAmt = d
	}
	trig := decimal.RequireFromString("0.15")
	if t := strings.TrimSpace(r.TriggerMMR); t != "" {
		d, err := decimal.NewFromString(t)
		if err != nil || d.LessThan(decimal.RequireFromString("0.05")) || d.GreaterThan(decimal.RequireFromString("0.5")) {
			return AutoMarginView{}, bad("VALIDATION_ERROR", "triggerRatio 必须在 5% 到 50% 之间")
		}
		trig = d
	}
	if r.Enabled && maxAmt.IsZero() {
		return AutoMarginView{}, bad("VALIDATION_ERROR", "启用自动追加保证金时必须填写 maxAmount")
	}
	var v AutoMarginView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		// Row may not exist yet; seed with the user's defaults.
		if _, err := tx.ExecContext(ctx, `INSERT INTO fut_settings (user_id, symbol, leverage, margin_mode) VALUES ($1,$2,$3,$4)
			ON CONFLICT (user_id, symbol) DO NOTHING`, uid, c.Symbol, s.cfg.DefaultLeverage, s.cfg.DefaultMarginMode); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE fut_settings SET auto_margin_enabled=$3, auto_margin_max=$4, auto_margin_trigger=$5,
			auto_margin_used = CASE WHEN auto_margin_enabled=false AND $3=true THEN 0 ELSE auto_margin_used END
			WHERE user_id=$1 AND symbol=$2`, uid, c.Symbol, r.Enabled, maxAmt, trig)
		if err != nil {
			return err
		}
		v, err = readAutoMargin(ctx, tx, uid, c.Symbol)
		return err
	})
	return v, err
}

func readAutoMargin(ctx context.Context, q queryer, uid, sym string) (AutoMarginView, error) {
	v := AutoMarginView{Symbol: sym, TriggerRatio: decimal.RequireFromString("0.15")}
	err := q.QueryRowContext(ctx, `SELECT auto_margin_enabled, auto_margin_max, auto_margin_used, auto_margin_trigger
		FROM fut_settings WHERE user_id=$1 AND symbol=$2`, uid, sym).Scan(&v.Enabled, &v.MaxAmount, &v.UsedAmount, &v.TriggerRatio)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	return v, err
}

// RunAutoMargin wakes every AutoMarginEvery, scans users with auto_margin
// enabled, and tops up thin isolated positions.
func (s *Server) RunAutoMargin(ctx context.Context) {
	if s.cfg.AutoMarginEvery <= 0 {
		return
	}
	t := time.NewTicker(s.cfg.AutoMarginEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.autoMarginSweep(ctx)
	}
}

func (s *Server) autoMarginSweep(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.user_id, s.symbol, s.auto_margin_max, s.auto_margin_used, s.auto_margin_trigger
		FROM fut_settings s WHERE s.auto_margin_enabled = true`)
	if err != nil {
		return
	}
	type row struct {
		uid, sym          string
		cap, used, trig   decimal.Decimal
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.uid, &r.sym, &r.cap, &r.used, &r.trig); err == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	for _, r := range list {
		if err := s.autoMarginUser(ctx, r.uid, r.sym, r.cap, r.used, r.trig); err != nil {
			log.Printf("futures: auto-margin %s %s: %v", r.uid, r.sym, err)
		}
	}
}

// autoMarginUser evaluates every isolated position for (uid, sym) and tops it up
// if the margin ratio sits below trigger. A top-up credits iso_margin from the
// wallet in one transaction and bumps auto_margin_used; a wallet shortfall or
// hitting the cap stops the top-up but leaves the row enabled.
func (s *Server) autoMarginUser(ctx context.Context, uid, sym string, cap, used, trig decimal.Decimal) error {
	c, err := s.contract(sym)
	if err != nil {
		return err
	}
	m, ok := s.mark(sym)
	if !ok {
		return nil
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		// Re-read cap/used under the lock so two sweeps can't double-spend.
		var enabled bool
		var capNow, usedNow decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT auto_margin_enabled, auto_margin_max, auto_margin_used FROM fut_settings WHERE user_id=$1 AND symbol=$2 FOR UPDATE`, uid, sym).Scan(&enabled, &capNow, &usedNow); err != nil {
			return err
		}
		if !enabled || capNow.LessThanOrEqual(usedNow) {
			return nil
		}
		remaining := capNow.Sub(usedNow)
		for _, side := range []string{"BOTH", "LONG", "SHORT"} {
			p, found, err := loadPosition(ctx, tx, uid, sym, side, true)
			if err != nil {
				return err
			}
			if !found || p.Qty.IsZero() || p.Mode != "isolated" {
				continue
			}
			// Maintenance margin and current equity on the position.
			mm := c.maintMargin(p.Qty.Abs().Mul(m))
			u := upnl(p.Qty, p.Entry, m)
			equity := p.Iso.Add(u)
			if equity.LessThanOrEqual(dZero) {
				// Liquidation territory — leave it to risk.go.
				continue
			}
			ratio := mm.Div(equity)
			if ratio.LessThan(trig) {
				continue
			}
			// Top up so the new ratio is trig + 5 pp (half the cushion that
			// Binance uses). Add the maths out in full so we never under-fund.
			target := trig.Add(decimal.RequireFromString("0.05"))
			need := mm.Div(target).Sub(equity)
			if need.Sign() <= 0 {
				continue
			}
			// Cap by remaining allowance and by wallet availability.
			st, err := s.state(ctx, tx, uid)
			if err != nil {
				return err
			}
			add := money(dmin(dmin(need, remaining), st.Available))
			if add.Sign() <= 0 {
				continue
			}
			p.Iso = p.Iso.Add(add)
			if err := savePosition(ctx, tx, p); err != nil {
				return err
			}
			ref := newRef("amgn")
			if err := addBalance(ctx, tx, uid, sym, "auto_margin", add.Neg(), ref); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE fut_settings SET auto_margin_used = auto_margin_used + $3 WHERE user_id=$1 AND symbol=$2`, uid, sym, add); err != nil {
				return err
			}
			usedNow = usedNow.Add(add)
			remaining = capNow.Sub(usedNow)
			if remaining.Sign() <= 0 {
				break
			}
		}
		return nil
	})
}

// resetAutoMarginUsed zeroes auto_margin_used when the position on sym closes.
// The fill path calls this after a flat/flip on the last open side.
func resetAutoMarginUsed(ctx context.Context, tx queryer, uid, sym string) error {
	_, err := tx.ExecContext(ctx, `UPDATE fut_settings SET auto_margin_used = 0 WHERE user_id=$1 AND symbol=$2 AND auto_margin_used > 0`, uid, sym)
	return err
}

func (s *Server) hAutoMargin(w http.ResponseWriter, r *http.Request, uid string) {
	sym := r.PathValue("symbol")
	if r.Method == http.MethodGet {
		v, err := readAutoMargin(r.Context(), s.db, uid, strings.ToUpper(sym))
		if err != nil {
			writeErr(w, err)
			return
		}
		v.Symbol = strings.ToUpper(sym)
		common.WriteJSON(w, http.StatusOK, map[string]any{"autoMargin": v})
		return
	}
	var req AutoMarginReq
	if !decode(w, r, &req) {
		return
	}
	v, err := s.SetAutoMargin(r.Context(), uid, sym, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	v.Symbol = strings.ToUpper(sym)
	common.WriteJSON(w, http.StatusOK, map[string]any{"autoMargin": v})
}
