package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// publicdata.go: public (no auth) data for the futures info pages — risk-limit
// tiers, isolated-margin parameters, index composition, mark-price history,
// funding-rate history and the insurance-fund balance history.

// timeRange reads startTime/endTime (unix ms, both optional).
func timeRange(r *http.Request) (start, end *time.Time, err error) {
	parse := func(k string) (*time.Time, error) {
		v := strings.TrimSpace(r.URL.Query().Get(k))
		if v == "" {
			return nil, nil
		}
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 0 {
			return nil, bad("INVALID_TIME", "时间参数不合法")
		}
		t := time.UnixMilli(n).UTC()
		return &t, nil
	}
	if start, err = parse("startTime"); err != nil {
		return
	}
	if end, err = parse("endTime"); err != nil {
		return
	}
	if start != nil && end != nil && end.Before(*start) {
		err = bad("INVALID_TIME", "结束时间早于开始时间")
	}
	return
}

var markIntervals = map[string]int64{"1m": 60, "5m": 300, "15m": 900, "30m": 1800, "1h": 3600, "4h": 14400, "1d": 86400}

type tierRow struct {
	Tier
	MinNotional decimal.Decimal `json:"minNotional"`
}

func tierRows(c Contract) []tierRow {
	ts := c.Tiers
	if len(ts) == 0 {
		ts = []Tier{c.tierFor(dZero)}
		ts[0].Tier = 1
		ts[0].MaxNotional = decimal.NewFromInt(1).Shift(18)
	}
	out := make([]tierRow, 0, len(ts))
	lo := dZero
	for _, t := range ts {
		out = append(out, tierRow{Tier: t, MinNotional: lo})
		lo = t.MaxNotional
	}
	return out
}

// selected returns the contract named by ?symbol= or every contract.
func (s *Server) selected(r *http.Request) ([]Contract, error) {
	if sym := strings.TrimSpace(r.URL.Query().Get("symbol")); sym != "" {
		c, err := s.contract(sym)
		if err != nil {
			return nil, err
		}
		return []Contract{c}, nil
	}
	out := make([]Contract, 0, len(s.symbols))
	for _, sym := range s.symbols {
		out = append(out, s.contracts[sym])
	}
	return out, nil
}

// GET /futures/market/risk-tiers[?symbol=]
func (s *Server) hRiskTiers(w http.ResponseWriter, r *http.Request) {
	cs, err := s.selected(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"symbol": c.Symbol, "marginAsset": c.Margin, "tiers": tierRows(c)})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"contracts": out})
}

// GET /futures/market/isolated-params[?symbol=]
func (s *Server) hIsolatedParams(w http.ResponseWriter, r *http.Request) {
	cs, err := s.selected(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		t1 := c.tierFor(dZero)
		out = append(out, map[string]any{
			"symbol": c.Symbol, "marginAsset": c.Margin, "marginModes": []string{"cross", "isolated"},
			"maxLeverage": c.MaxLeverage, "defaultLeverage": min(s.cfg.DefaultLeverage, c.MaxLeverage),
			"maintenanceMarginRate": t1.MMR, "liquidationFeeRate": s.liqFeeRate(c),
			"minQty": c.MinQty, "maxQty": c.MaxQty, "tickSize": c.Tick,
			"makerFeeRate": c.MakerFee, "takerFeeRate": c.TakerFee,
			"fundingIntervalHours": c.FundingH, "marginAdjustable": true, "tiers": tierRows(c),
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"contracts": out, "defaultMarginMode": s.cfg.DefaultMarginMode})
}

// GET /futures/market/index?symbol= — current index/mark plus the
// constituent sources reported by market-data (Binance primary, OKX backup;
// the active source carries weight 1).
func (s *Server) hIndex(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	ps := s.snapshot(c.Symbol)
	res := map[string]any{"symbol": c.Symbol, "indexPrice": ps.Index, "markPrice": ps.Mark, "method": "primary_failover", "components": []any{}}
	if !ps.IndexAt.IsZero() {
		res["indexUpdatedAt"] = ps.IndexAt.UnixMilli()
	}
	if info, ok := s.market.IndexInfo(r.Context(), c.Base); ok {
		res["source"] = info.Source
		if len(info.Components) > 0 {
			res["components"] = info.Components
		}
	}
	common.WriteJSON(w, http.StatusOK, res)
}

// GET /futures/market/funding-history?symbol=&startTime=&endTime=&limit=
// Newest first; page back with endTime = nextEndTime.
func (s *Server) hFundingHistory(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	st, en, err := timeRange(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	lim := limitParam(r, 100, 1000)
	rows, err := s.db.QueryContext(r.Context(), `SELECT funding_time, rate, mark_price, source FROM fut_funding_history
		WHERE symbol=$1 AND ($2::timestamptz IS NULL OR funding_time >= $2) AND ($3::timestamptz IS NULL OR funding_time <= $3)
		ORDER BY funding_time DESC LIMIT $4`, c.Symbol, st, en, lim)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	var last time.Time
	for rows.Next() {
		var at time.Time
		var rate, mark decimal.Decimal
		var src string
		if rows.Scan(&at, &rate, &mark, &src) == nil {
			last = at
			out = append(out, map[string]any{"fundingTime": at.UnixMilli(), "fundingRate": rate, "markPrice": mark, "source": src})
		}
	}
	res := map[string]any{"symbol": c.Symbol, "fundingIntervalHours": c.FundingH, "history": out}
	if len(out) == lim {
		res["nextEndTime"] = last.UnixMilli() - 1
	}
	common.WriteJSON(w, http.StatusOK, res)
}

// GET /futures/market/mark-history?symbol=&interval=&startTime=&endTime=&limit=
// Without interval: raw samples {time,indexPrice,markPrice}. With interval:
// mark-price klines {openTime,open,high,low,close,indexClose}. Newest first.
func (s *Server) hMarkHistory(w http.ResponseWriter, r *http.Request) {
	c, err := s.contract(r.URL.Query().Get("symbol"))
	if err != nil {
		writeErr(w, err)
		return
	}
	st, en, err := timeRange(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	lim := limitParam(r, 200, 2000)
	iv := strings.TrimSpace(r.URL.Query().Get("interval"))
	out := []map[string]any{}
	var oldest time.Time
	if iv == "" {
		rows, err := s.db.QueryContext(r.Context(), `SELECT ts, index_price, mark_price FROM fut_mark_history
			WHERE symbol=$1 AND ($2::timestamptz IS NULL OR ts >= $2) AND ($3::timestamptz IS NULL OR ts <= $3)
			ORDER BY ts DESC LIMIT $4`, c.Symbol, st, en, lim)
		if err != nil {
			writeErr(w, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var at time.Time
			var ip, mp decimal.Decimal
			if rows.Scan(&at, &ip, &mp) == nil {
				oldest = at
				out = append(out, map[string]any{"time": at.UnixMilli(), "indexPrice": ip, "markPrice": mp})
			}
		}
	} else {
		sec, ok := markIntervals[iv]
		if !ok {
			writeErr(w, bad("INVALID_INTERVAL", "周期不支持"))
			return
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT to_timestamp(floor(extract(epoch FROM ts)/$5)*$5) AS b,
			(array_agg(mark_price ORDER BY ts))[1], max(mark_price), min(mark_price),
			(array_agg(mark_price ORDER BY ts DESC))[1], (array_agg(index_price ORDER BY ts DESC))[1]
			FROM fut_mark_history
			WHERE symbol=$1 AND ($2::timestamptz IS NULL OR ts >= $2) AND ($3::timestamptz IS NULL OR ts <= $3)
			GROUP BY b ORDER BY b DESC LIMIT $4`, c.Symbol, st, en, lim, sec)
		if err != nil {
			writeErr(w, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var at time.Time
			var o, h, l, cl, ic decimal.Decimal
			if rows.Scan(&at, &o, &h, &l, &cl, &ic) == nil {
				oldest = at
				out = append(out, map[string]any{"openTime": at.UnixMilli(), "open": o, "high": h, "low": l, "close": cl, "indexClose": ic})
			}
		}
	}
	res := map[string]any{"symbol": c.Symbol, "interval": iv, "sampleSeconds": int(s.cfg.MarkSampleEvery / time.Second), "history": out}
	if len(out) == lim {
		res["nextEndTime"] = oldest.UnixMilli() - 1
	}
	common.WriteJSON(w, http.StatusOK, res)
}

// GET /futures/market/insurance-fund/history?startTime=&endTime=&limit=
func (s *Server) hInsuranceHistory(w http.ResponseWriter, r *http.Request) {
	st, en, err := timeRange(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	lim := limitParam(r, 200, 2000)
	rows, err := s.db.QueryContext(r.Context(), `SELECT ts, asset, balance, equity FROM fut_insurance_snapshots
		WHERE ($1::timestamptz IS NULL OR ts >= $1) AND ($2::timestamptz IS NULL OR ts <= $2)
		ORDER BY ts DESC LIMIT $3`, st, en, lim)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	var oldest time.Time
	for rows.Next() {
		var at time.Time
		var asset string
		var bal, eq decimal.Decimal
		if rows.Scan(&at, &asset, &bal, &eq) == nil {
			oldest = at
			out = append(out, map[string]any{"time": at.UnixMilli(), "asset": asset, "balance": money(bal), "equity": money(eq)})
		}
	}
	res := map[string]any{"asset": "USDT", "snapshotMinutes": int(s.cfg.InsuranceSnapEvery / time.Minute), "history": out}
	if len(out) == lim {
		res["nextEndTime"] = oldest.UnixMilli() - 1
	}
	common.WriteJSON(w, http.StatusOK, res)
}

// snapshotInsurance records the insurance-fund balance once per
// InsuranceSnapEvery bucket (idempotent across replicas via the PK).
func (s *Server) snapshotInsurance(ctx context.Context, force bool) {
	every := s.cfg.InsuranceSnapEvery
	if every <= 0 {
		every = time.Hour
	}
	now := s.now().UTC()
	b := now.Truncate(every)
	if !force && !s.lastInsSnap.IsZero() && !b.After(s.lastInsSnap) {
		return
	}
	bal, _, err := readAccount(ctx, s.db, insuranceUser)
	if err != nil {
		return
	}
	eq := bal
	if st, err := s.state(ctx, s.db, insuranceUser); err == nil {
		eq = st.Equity
	}
	if force {
		b = now.Truncate(time.Second)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO fut_insurance_snapshots (ts, asset, balance, equity) VALUES ($1,'USDT',$2,$3) ON CONFLICT DO NOTHING`, b, bal, eq); err == nil {
		s.lastInsSnap = now.Truncate(every)
	}
}
