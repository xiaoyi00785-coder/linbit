package main

// GET /futures/pnl/history：按日已实现盈亏，来自 fut_bills，另加成交笔数。
// 没有账单的日子是 0，表示没有对应流水，不是抽样曲线。

import (
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

var pnlKinds = []string{"realized_pnl", "funding", "fee", "liquidation_fee", "insurance_cover"}

func parseHistoryRange(fromS, toS string, now time.Time) (time.Time, time.Time, error) {
	now = now.UTC()
	to, err := parseRangeBound(toS, true, now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, err := parseRangeBound(fromS, false, to.AddDate(0, 0, -30))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, bad("VALIDATION_ERROR", "时间范围不合法（最多 366 天，from 必须早于 to）")
	}
	return from, to, nil
}

func parseRangeBound(s string, dateIsEnd bool, def time.Time) (time.Time, error) {
	s = trim(s)
	if s == "" {
		return def.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, bad("VALIDATION_ERROR", "时间须为 RFC3339 或 YYYY-MM-DD")
	}
	if dateIsEnd {
		return d.Add(24 * time.Hour).UTC(), nil
	}
	return d.UTC(), nil
}

func trim(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

type pnlPoint struct {
	Date           string `json:"date"`
	Pnl            string `json:"pnl"`
	RealizedPnl    string `json:"realizedPnl"`
	Funding        string `json:"funding"`
	Fee            string `json:"fee"`
	LiquidationFee string `json:"liquidationFee"`
	InsuranceCover string `json:"insuranceCover"`
	TradeCount     int    `json:"tradeCount"`
}

func (s *Server) hPnlHistory(w http.ResponseWriter, r *http.Request, uid string) {
	from, to, err := parseHistoryRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), s.now())
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), kind, COALESCE(sum(amount),0)
		FROM fut_bills WHERE user_id=$1 AND created_at >= $2 AND created_at < $3
		AND kind IN ('realized_pnl','funding','fee','liquidation_fee','insurance_cover')
		GROUP BY 1, 2`, uid, from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	byDay := map[string]map[string]decimal.Decimal{}
	for rows.Next() {
		var day, kind string
		var amt decimal.Decimal
		if err := rows.Scan(&day, &kind, &amt); err != nil {
			writeErr(w, err)
			return
		}
		if byDay[day] == nil {
			byDay[day] = map[string]decimal.Decimal{}
		}
		byDay[day][kind] = amt
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}
	trows, err := s.db.QueryContext(r.Context(), `SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), count(*)
		FROM fut_trades WHERE (maker_user=$1 OR taker_user=$1) AND created_at >= $2 AND created_at < $3 GROUP BY 1`, uid, from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer trows.Close()
	trades := map[string]int{}
	for trows.Next() {
		var day string
		var n int
		if err := trows.Scan(&day, &n); err != nil {
			writeErr(w, err)
			return
		}
		trades[day] = n
	}
	if err := trows.Err(); err != nil {
		writeErr(w, err)
		return
	}
	points := []pnlPoint{}
	totals := map[string]decimal.Decimal{}
	tradeTotal := 0
	start := time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for d := start; d.Before(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		m := byDay[key]
		p := pnlPoint{Date: key, TradeCount: trades[key]}
		sum := decimal.Zero
		vals := map[string]decimal.Decimal{}
		for _, k := range pnlKinds {
			v := m[k]
			vals[k] = v
			sum = sum.Add(v)
			totals[k] = totals[k].Add(v)
		}
		totals["pnl"] = totals["pnl"].Add(sum)
		tradeTotal += p.TradeCount
		p.Pnl = feeStr(sum)
		p.RealizedPnl = feeStr(vals["realized_pnl"])
		p.Funding = feeStr(vals["funding"])
		p.Fee = feeStr(vals["fee"])
		p.LiquidationFee = feeStr(vals["liquidation_fee"])
		p.InsuranceCover = feeStr(vals["insurance_cover"])
		points = append(points, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"asset": "USDT", "from": from.UTC().Format(time.RFC3339), "to": to.UTC().Format(time.RFC3339),
		"realizedPnlAvailable": true,
		"points":               points,
		"totals": map[string]any{
			"pnl": feeStr(totals["pnl"]), "realizedPnl": feeStr(totals["realized_pnl"]), "funding": feeStr(totals["funding"]),
			"fee": feeStr(totals["fee"]), "liquidationFee": feeStr(totals["liquidation_fee"]), "insuranceCover": feeStr(totals["insurance_cover"]),
			"tradeCount": tradeTotal,
		},
	})
}
