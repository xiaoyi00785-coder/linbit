package main

// GET /spot/pnl/history：按日现货成交统计。现货成交没有成本，已实现盈亏固定为不可用，不拿成交额冒充。
// 计价成交额只计 USDT 交易对。USDT 手续费和成交额用 Round(8).StringFixed(8)；非 USDT 手续费的那一天 feeAvailable=false，不混进合计。

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func parseSpotRange(fromS, toS string, now time.Time) (time.Time, time.Time, error) {
	now = now.UTC()
	to, err := parseSpotBound(toS, true, now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, err := parseSpotBound(fromS, false, to.AddDate(0, 0, -30))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, errValidation("时间范围不合法（最多 366 天，from 必须早于 to）")
	}
	return from, to, nil
}

func parseSpotBound(s string, dateIsEnd bool, def time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errValidation("时间须为 RFC3339 或 YYYY-MM-DD")
	}
	if dateIsEnd {
		return d.Add(24 * time.Hour).UTC(), nil
	}
	return d.UTC(), nil
}

func errValidation(msg string) error { return validationError(msg) }

type validationError string

func (e validationError) Error() string { return string(e) }

func (s *Server) handleSpotPnlHistory(w http.ResponseWriter, r *http.Request, uid string) {
	from, to, err := parseSpotRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), time.Now())
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'),
		count(*) FILTER (WHERE symbol LIKE '%USDT'),
		count(*) FILTER (WHERE symbol NOT LIKE '%USDT'),
		COALESCE(sum(price*quantity) FILTER (WHERE symbol LIKE '%USDT'), 0)::text,
		COALESCE(sum(fee) FILTER (WHERE symbol LIKE '%USDT' AND (fee_asset IS NULL OR fee_asset='' OR fee_asset='USDT')), 0)::text,
		count(*) FILTER (WHERE symbol LIKE '%USDT' AND fee_asset IS NOT NULL AND fee_asset<>'' AND fee_asset<>'USDT')
		FROM trades WHERE user_id=$1 AND created_at >= $2 AND created_at < $3 GROUP BY 1`, uid, from, to)
	if err != nil {
		log.Printf("spot pnl history: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	type day struct {
		usdtN, otherN int
		quote, fee    decimal.Decimal
		foreignFee    int
	}
	got := map[string]day{}
	for rows.Next() {
		var key, quote, fee string
		var d day
		if err := rows.Scan(&key, &d.usdtN, &d.otherN, &quote, &fee, &d.foreignFee); err != nil {
			log.Printf("spot pnl history scan: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		d.quote, _ = decimal.NewFromString(quote)
		d.fee, _ = decimal.NewFromString(fee)
		got[key] = d
	}
	if err := rows.Err(); err != nil {
		log.Printf("spot pnl history rows: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	points := []map[string]any{}
	var quoteSum, feeSum decimal.Decimal
	usdtN, otherN := 0, 0
	feeOK := true
	start := time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for d := start; d.Before(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		row := got[key]
		ok := row.foreignFee == 0
		if !ok {
			feeOK = false
		}
		var fee any
		// USDT 手续费四舍五入到 8 位再固定 8 位展示。非 USDT 手续费不折算，整天标 feeAvailable=false。
		if ok {
			fee = row.fee.Round(8).StringFixed(8)
			feeSum = feeSum.Add(row.fee)
		}
		points = append(points, map[string]any{
			"date": key, "tradeCount": row.usdtN, "nonUsdtTradeCount": row.otherN,
			"quote": "USDT", "quoteVolume": row.quote.Round(8).StringFixed(8),
			"fee": fee, "feeAvailable": ok, "realizedPnl": nil, "realizedPnlAvailable": false,
		})
		quoteSum = quoteSum.Add(row.quote)
		usdtN += row.usdtN
		otherN += row.otherN
	}
	var totalFee any
	if feeOK {
		totalFee = feeSum.Round(8).StringFixed(8)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"from": from.UTC().Format(time.RFC3339), "to": to.UTC().Format(time.RFC3339),
		"realizedPnlAvailable": false, "realizedPnl": nil,
		"reason": "现货成交未记录持仓成本，不能把成交额当成已实现盈亏",
		"points": points,
		"totals": map[string]any{
			"tradeCount": usdtN, "nonUsdtTradeCount": otherN, "quote": "USDT",
			"quoteVolume": quoteSum.Round(8).StringFixed(8), "fee": totalFee, "feeAvailable": feeOK,
			"realizedPnl": nil, "realizedPnlAvailable": false,
		},
	})
}
