package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// event_admin.go: internal endpoints behind admin /admin/v1/event (the admin
// service enforces RBAC, 2FA, reason and audit; writes return before/after).

func (s *Server) evAdminState(r *http.Request) (map[string]any, error) {
	cfg, err := loadEvConfig(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	syms, err := loadEvSymbols(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	return map[string]any{"config": cfg, "symbols": syms}, nil
}

func (s *Server) hEvAdminConfig(w http.ResponseWriter, r *http.Request) {
	v, err := s.evAdminState(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, v)
}

// PUT /internal/event/config {periods?, payoutRatio?, minStake?, maxStake?,
// dailyLimit?, exposureLimit?, settleGraceSeconds?, updatedBy}. Changes apply
// to new orders only; open orders keep their recorded payoutRatio.
func (s *Server) hEvAdminConfigPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Periods        []string `json:"periods"`
		PayoutRatio    *string  `json:"payoutRatio"`
		MinStake       *string  `json:"minStake"`
		MaxStake       *string  `json:"maxStake"`
		DailyLimit     *string  `json:"dailyLimit"`
		ExposureLimit  *string  `json:"exposureLimit"`
		SettleGraceSec *int     `json:"settleGraceSeconds"`
		UpdatedBy      string   `json:"updatedBy"`
	}
	if !decode(w, r, &in) {
		return
	}
	before, err := loadEvConfig(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	c := before
	if in.Periods != nil {
		c.Periods = nil
		for _, p := range in.Periods {
			c.Periods = append(c.Periods, strings.ToLower(strings.TrimSpace(p)))
		}
	}
	for _, f := range []struct {
		v   *string
		dst *decimal.Decimal
		n   string
	}{{in.PayoutRatio, &c.PayoutRatio, "payoutRatio"}, {in.MinStake, &c.MinStake, "minStake"}, {in.MaxStake, &c.MaxStake, "maxStake"},
		{in.DailyLimit, &c.DailyLimit, "dailyLimit"}, {in.ExposureLimit, &c.ExposureLimit, "exposureLimit"}} {
		if f.v == nil {
			continue
		}
		d, err := decimal.NewFromString(strings.TrimSpace(*f.v))
		if err != nil {
			writeErr(w, bad("VALIDATION_ERROR", f.n+" 不合法"))
			return
		}
		*f.dst = d
	}
	if in.SettleGraceSec != nil {
		c.SettleGraceSec = *in.SettleGraceSec
	}
	if err := c.validate(); err != nil {
		writeErr(w, err)
		return
	}
	by := strings.TrimSpace(in.UpdatedBy)
	if by == "" {
		by = "admin"
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE ev_config SET periods=$1, payout_ratio=$2, min_stake=$3, max_stake=$4, daily_limit=$5, exposure_limit=$6, settle_grace_s=$7, updated_by=$8, updated_at=now() WHERE id=1`,
		textArray(c.Periods), c.PayoutRatio, c.MinStake, c.MaxStake, c.DailyLimit, c.ExposureLimit, c.SettleGraceSec, by); err != nil {
		writeErr(w, err)
		return
	}
	after, err := loadEvConfig(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"config": after, "before": before, "after": after})
}

// PUT /internal/event/symbols/{symbol} {paused?, exposureLimit? ("" or null =
// use the global default), updatedBy}
func (s *Server) hEvAdminSymbolPut(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(r.PathValue("symbol"))
	var in struct {
		Paused        *bool   `json:"paused"`
		ExposureLimit *string `json:"exposureLimit"`
		ClearLimit    bool    `json:"clearExposureLimit"`
		UpdatedBy     string  `json:"updatedBy"`
	}
	if !evSymbolRe.MatchString(sym) || !decode(w, r, &in) {
		if !evSymbolRe.MatchString(sym) {
			writeErr(w, bad("VALIDATION_ERROR", "symbol 不合法"))
		}
		return
	}
	var before *evSymbol
	syms, err := loadEvSymbols(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	for i := range syms {
		if syms[i].Symbol == sym {
			before = &syms[i]
		}
	}
	if before == nil {
		writeErr(w, errNotFound)
		return
	}
	after := *before
	if in.Paused != nil {
		after.Paused = *in.Paused
	}
	if in.ClearLimit || (in.ExposureLimit != nil && strings.TrimSpace(*in.ExposureLimit) == "") {
		after.ExposureLimit = decimal.NullDecimal{}
	} else if in.ExposureLimit != nil {
		d, err := decimal.NewFromString(strings.TrimSpace(*in.ExposureLimit))
		if err != nil || d.Sign() <= 0 {
			writeErr(w, bad("VALIDATION_ERROR", "exposureLimit 需大于 0"))
			return
		}
		after.ExposureLimit = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	by := strings.TrimSpace(in.UpdatedBy)
	if by == "" {
		by = "admin"
	}
	var lim any
	if after.ExposureLimit.Valid {
		lim = after.ExposureLimit.Decimal
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE ev_symbols SET paused=$2, exposure_limit=$3, updated_by=$4, updated_at=now() WHERE symbol=$1`, sym, after.Paused, lim, by); err != nil {
		writeErr(w, err)
		return
	}
	after.UpdatedBy, after.UpdatedAt = by, s.now().UTC()
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": after, "before": before, "after": after})
}

// GET /internal/event/orders?uid=&symbol=&status=&before=&limit=
func (s *Server) hEvAdminOrders(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + evCols + ` FROM ev_orders WHERE true`
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(" AND "+cond, len(args))
	}
	qs := r.URL.Query()
	if v := strings.TrimSpace(qs.Get("uid")); v != "" {
		add("user_id = $%d", v)
	}
	if v := strings.ToUpper(strings.TrimSpace(qs.Get("symbol"))); v != "" {
		add("symbol = $%d", v)
	}
	if v := strings.TrimSpace(qs.Get("status")); v != "" && v != "all" {
		if v == "open" {
			q += ` AND status IN ('pending','open','settling')`
		} else {
			add("status = $%d", v)
		}
	}
	if b, err := strconv.ParseInt(qs.Get("before"), 10, 64); err == nil && b > 0 {
		add("id < $%d", b)
	}
	lim := limitParam(r, 50, 500)
	q += fmt.Sprintf(` ORDER BY id DESC LIMIT %d`, lim)
	list, err := s.queryEv(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, o := range list {
		out = append(out, o.dto(true))
	}
	next := ""
	if len(list) == lim {
		next = strconv.FormatInt(list[len(list)-1].ID, 10)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"orders": out, "nextBefore": next})
}

// GET /internal/event/exposure: unsettled stakes per symbol x period plus
// today's realised platform P&L (stakes kept minus payouts above stake).
func (s *Server) hEvAdminExposure(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadEvConfig(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	syms, err := loadEvSymbols(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	type key struct{ sym, per string }
	agg := map[key][3]decimal.Decimal{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT symbol, period, COALESCE(sum(stake) FILTER (WHERE direction='up'),0), COALESCE(sum(stake) FILTER (WHERE direction='down'),0), count(*)
		FROM ev_orders WHERE status IN ('pending','open') GROUP BY symbol, period`)
	if err != nil {
		writeErr(w, err)
		return
	}
	for rows.Next() {
		var k key
		var up, down decimal.Decimal
		var n int64
		if err := rows.Scan(&k.sym, &k.per, &up, &down, &n); err != nil {
			rows.Close()
			writeErr(w, err)
			return
		}
		agg[k] = [3]decimal.Decimal{up, down, decimal.NewFromInt(n)}
	}
	rows.Close()
	out := []map[string]any{}
	for _, e := range syms {
		limit := cfg.ExposureLimit
		if e.ExposureLimit.Valid {
			limit = e.ExposureLimit.Decimal
		}
		pers := append([]string{}, cfg.Periods...)
		for k := range agg { // periods removed from the config but still open
			if k.sym == e.Symbol && !contains(pers, k.per) {
				pers = append(pers, k.per)
			}
		}
		for _, p := range pers {
			a := agg[key{e.Symbol, p}]
			net := a[0].Sub(a[1])
			out = append(out, map[string]any{"symbol": e.Symbol, "period": p, "paused": e.Paused, "upStake": a[0].StringFixed(2), "downStake": a[1].StringFixed(2),
				"netExposure": net.StringFixed(2), "limit": limit.String(), "openOrders": a[2].IntPart(),
				"utilization": ratio(net.Abs(), limit)})
		}
	}
	var stakes, payouts decimal.Decimal
	_ = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(sum(stake),0), COALESCE(sum(payout),0) FROM ev_orders WHERE status='settled' AND settled_at >= $1`, s.now().UTC().Truncate(24*time.Hour)).Scan(&stakes, &payouts)
	var stuck int64
	_ = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM ev_orders WHERE (status='pending' AND updated_at < now() - interval '1 minute') OR status='settling'`).Scan(&stuck)
	common.WriteJSON(w, http.StatusOK, map[string]any{"books": out, "todayStakeSettled": stakes.StringFixed(2), "todayPayout": payouts.StringFixed(2),
		"todayPlatformPnl": stakes.Sub(payouts).StringFixed(2), "unfinishedOrders": stuck})
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func ratio(a, b decimal.Decimal) string {
	if b.Sign() <= 0 {
		return "0"
	}
	return a.Div(b).StringFixed(4)
}
