package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Maker rebate. Every fill that g_fee_events records with role='maker' feeds
// into a daily per-user accrual (g_rebate_accruals) at a rate from
// g_rebate_rules keyed by the user's VIP level. The daily settle walks those
// accruals and pays USDT to the user's spot via systemTransfer.

type rebateRule struct {
	VIPLevel    int             `json:"vipLevel"`
	Name        string          `json:"name"`
	MakerRebate decimal.Decimal `json:"makerRebate"`
	MinVolume   decimal.Decimal `json:"minVolume30d"`
	Enabled     bool            `json:"enabled"`
}

func (s *Server) rebateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /rebate/rules", s.handleRebateRules)
	mux.HandleFunc("GET /rebate/me", s.user(s.handleRebateMe))
	mux.HandleFunc("GET /rebate/accruals", s.user(s.handleRebateAccruals))
	mux.HandleFunc("POST /internal/dev/growth/rebate/accrue", s.dev(func(w http.ResponseWriter, r *http.Request) {
		day := utcDay(s.now().Add(-24 * time.Hour))
		if v := r.URL.Query().Get("day"); v != "" {
			if d, err := time.Parse("2006-01-02", v); err == nil {
				day = utcDay(d)
			}
		}
		n, err := s.accrueRebatesForDay(r.Context(), day)
		if err != nil {
			writeErr(w, err)
			return
		}
		paid, err := s.payPendingRebates(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"day": day.Format("2006-01-02"), "accruals": n, "paid": paid})
	}))
}

func (s *Server) loadRebateRules(ctx context.Context) ([]rebateRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT vip_level, name, maker_rebate, min_volume, enabled FROM g_rebate_rules ORDER BY vip_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []rebateRule{}
	for rows.Next() {
		var r rebateRule
		if err := rows.Scan(&r.VIPLevel, &r.Name, &r.MakerRebate, &r.MinVolume, &r.Enabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Server) handleRebateRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadRebateRules(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "basis": "maker 手续费的比例返还，次日结算至现货账户"})
}

func (s *Server) handleRebateMe(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	period := r.URL.Query().Get("period")
	since := s.now().Add(-30 * 24 * time.Hour)
	if period == "last_7d" {
		since = s.now().Add(-7 * 24 * time.Hour)
	}
	var makerVol, makerFee, total, pending decimal.Decimal
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(notional),0), COALESCE(SUM(fee),0) FROM g_fee_events WHERE uid=$1 AND role='maker' AND at >= $2`, uid, since).Scan(&makerVol, &makerFee)
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_rebate_accruals WHERE uid=$1 AND period >= $2`, uid, utcDay(since)).Scan(&total)
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_rebate_accruals WHERE uid=$1 AND status='pending'`, uid).Scan(&pending)
	level := s.fetchVIPLevel(ctx, uid)
	var rule rebateRule
	if rules, err := s.loadRebateRules(ctx); err == nil {
		for _, rl := range rules {
			if rl.VIPLevel == level {
				rule = rl
				break
			}
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"uid": uid, "period": period, "vipLevel": level, "makerVolume": makerVol.String(),
		"makerFee": makerFee.Round(8).String(), "rebateRate": rule.MakerRebate.String(),
		"totalRebate": total.Round(8).String(), "pendingRebate": pending.Round(8).String(),
		"asset": s.cfg.PayoutAsset,
	})
}

func (s *Server) handleRebateAccruals(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT period, vip_level, maker_volume, maker_fee, rebate_rate, amount, status, paid_at
		FROM g_rebate_accruals WHERE uid=$1 ORDER BY period DESC LIMIT 60`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var d time.Time
		var level int
		var vol, fee, amt decimal.Decimal
		var rate decimal.Decimal
		var status string
		var paid sql.NullTime
		if err := rows.Scan(&d, &level, &vol, &fee, &rate, &amt, &status, &paid); err != nil {
			writeErr(w, err)
			return
		}
		item := map[string]any{"day": d.Format("2006-01-02"), "vipLevel": level,
			"makerVolume": vol.String(), "makerFee": fee.Round(8).String(),
			"rate": rate.String(), "amount": amt.Round(8).String(), "status": status}
		if paid.Valid {
			item["paidAt"] = paid.Time.UnixMilli()
		}
		items = append(items, item)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// fetchVIPLevel asks the account service for the user's current VIP tier.
// Falls back to 0 when unreachable. Called at most once per accrual per user,
// so a per-call HTTP round trip is acceptable.
func (s *Server) fetchVIPLevel(ctx context.Context, uid string) int {
	if s.cfg.AccountURL == "" {
		return 0
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var out struct {
		Level int `json:"level"`
	}
	// The /vip/me endpoint requires user auth; use the internal fee-rate probe
	// which is internal-token gated and carries the level.
	if err := s.call(cctx, "GET", s.cfg.AccountURL+"/internal/vip/fee-rate?uid="+url.QueryEscape(uid)+"&product=spot", nil, &out); err != nil {
		return 0
	}
	return out.Level
}

// accrueRebatesForDay walks g_fee_events for `day` and writes per-user
// accrual rows. Idempotent (UNIQUE uid, period) — reruns update the figures.
func (s *Server) accrueRebatesForDay(ctx context.Context, day time.Time) (int, error) {
	from := utcDay(day)
	to := from.Add(24 * time.Hour)
	rows, err := s.db.QueryContext(ctx, `SELECT uid, COALESCE(SUM(notional),0), COALESCE(SUM(fee-bonus_fee-coupon_fee),0)
		FROM g_fee_events WHERE role='maker' AND at >= $1 AND at < $2 AND fee > 0 GROUP BY uid`, from, to)
	if err != nil {
		return 0, err
	}
	type agg struct {
		uid string
		vol decimal.Decimal
		fee decimal.Decimal
	}
	var aggs []agg
	for rows.Next() {
		var a agg
		if err := rows.Scan(&a.uid, &a.vol, &a.fee); err != nil {
			rows.Close()
			return 0, err
		}
		aggs = append(aggs, a)
	}
	rows.Close()
	rules, err := s.loadRebateRules(ctx)
	if err != nil {
		return 0, err
	}
	ruleByLevel := map[int]rebateRule{}
	for _, r := range rules {
		ruleByLevel[r.VIPLevel] = r
	}
	n := 0
	for _, a := range aggs {
		if a.fee.Sign() <= 0 {
			continue
		}
		level := s.fetchVIPLevel(ctx, a.uid)
		rule, ok := ruleByLevel[level]
		if !ok || !rule.Enabled || rule.MakerRebate.Sign() <= 0 {
			continue
		}
		amount := a.fee.Mul(rule.MakerRebate)
		if _, err := s.db.ExecContext(ctx, `INSERT INTO g_rebate_accruals (uid, period, vip_level, maker_volume, maker_fee, rebate_rate, amount, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'pending')
			ON CONFLICT (uid, period) DO UPDATE SET vip_level=EXCLUDED.vip_level, maker_volume=EXCLUDED.maker_volume,
				maker_fee=EXCLUDED.maker_fee, rebate_rate=EXCLUDED.rebate_rate,
				amount=CASE WHEN g_rebate_accruals.status='pending' THEN EXCLUDED.amount ELSE g_rebate_accruals.amount END`,
			a.uid, from, level, a.vol.Round(8), a.fee.Round(12), rule.MakerRebate, amount.Round(12)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Server) payPendingRebates(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, uid, amount FROM g_rebate_accruals WHERE status='pending' AND amount > 0 LIMIT 500`)
	if err != nil {
		return 0, err
	}
	type row struct {
		id  int64
		uid string
		amt decimal.Decimal
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.uid, &r.amt); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, r)
	}
	rows.Close()
	paid := 0
	for _, r := range list {
		ref := "rebate:" + strconv.FormatInt(r.id, 10)
		if err := s.systemTransfer(ctx, r.uid, s.cfg.PayoutAsset, "spot", "credit", r.amt, ref); err != nil {
			// Transient failures stay pending and are retried next tick.
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE g_rebate_accruals SET status='paid', paid_at=now() WHERE id=$1 AND status='pending'`, r.id); err != nil {
			return paid, err
		}
		paid++
	}
	return paid, nil
}

// rebateAfterSync runs with each sync: accrues yesterday's maker rebates (so
// rate changes at midnight are captured) and pays any pending rows.
func (s *Server) rebateAfterSync(ctx context.Context) error {
	if _, err := s.accrueRebatesForDay(ctx, s.now().Add(-24*time.Hour)); err != nil {
		return err
	}
	_, err := s.payPendingRebates(ctx)
	return err
}

// decodeRebateRuleJSON kept as a helper for admin seeding scripts.
func decodeRebateRuleJSON(b []byte) (rebateRule, error) {
	var r rebateRule
	err := json.Unmarshal(b, &r)
	if err == nil && r.MakerRebate.Sign() < 0 {
		return r, errors.New("negative rebate")
	}
	return r, err
}
