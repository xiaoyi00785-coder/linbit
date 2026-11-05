package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// volume.go: GET /internal/growth/volume?uid=&days=30 — a user's own trading
// volume in USDT over the trailing window, for service-to-service decisions
// (account's VIP eligibility check). It reads g_fee_events, the same table the
// partner tiers and points use: spot fills of USD-quoted pairs
// (GROWTH_USD_QUOTES) plus all futures fills, synced every GROWTH_SYNC_MS.

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
