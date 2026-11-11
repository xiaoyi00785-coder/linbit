package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// GET /internal/growth/volume?uid=&days=30。用户自己在窗口内的 USDT 成交额，给 account 判 VIP。
// 读 g_fee_events，和合伙人档位、积分同一张表：GROWTH_USD_QUOTES 计价的现货，加上全部合约成交。
// 数据按 GROWTH_SYNC_MS 同步。days 默认 30，允许 1 到 365。

const (
	volumeDefaultDays = 30
	volumeMaxDays     = 365
)

// parseVolumeDays validates the days query value ("" -> 30).
func parseVolumeDays(v string) (int, bool) {
	if v == "" {
		return volumeDefaultDays, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > volumeMaxDays {
		return 0, false
	}
	return n, true
}

// userVolume sums spot and futures notional (USDT) for uid since `since`.
func (s *Server) userVolume(r *http.Request, uid string, since time.Time) (spot, futures decimal.Decimal, err error) {
	err = s.db.QueryRowContext(r.Context(), `SELECT
		COALESCE(SUM(notional) FILTER (WHERE product='spot'),0),
		COALESCE(SUM(notional) FILTER (WHERE product='futures'),0)
		FROM g_fee_events WHERE uid=$1 AND at >= $2`, uid, since).Scan(&spot, &futures)
	return
}

func (s *Server) handleInternalVolume(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	days, ok := parseVolumeDays(r.URL.Query().Get("days"))
	if uid == "" || !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "uid required; days must be 1-365")
		return
	}
	spot, fut, err := s.userVolume(r, uid, s.now().Add(-time.Duration(days)*24*time.Hour))
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"uid": uid, "days": days, "spotUsdt": spot.String(), "futuresUsdt": fut.String(), "totalUsdt": spot.Add(fut).String(),
	})
}
