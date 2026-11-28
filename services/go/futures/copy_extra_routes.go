package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// hCopyTraderPositions serves GET /futures/copy/traders/{id}/positions.
// Closed history is selected when the query has status=closed (original) or
// the App/compat spelling position_status=close. Otherwise open positions
// are returned. Privacy hides both from non-owner viewers.
func (s *Server) hCopyTraderPositions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.getTrader(ctx, r.PathValue("id"))
	if err == nil && t.Status == "pending" {
		err = bad("TRADER_NOT_FOUND", "交易员不存在")
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	viewer := r.Header.Get("X-User-Id")
	if t.PositionPrivacy && viewer != t.UserID {
		common.WriteJSON(w, http.StatusOK, map[string]any{"positions": []any{}, "positionPrivacy": true})
		return
	}
	if copyPositionsWantClosed(r.URL.Query()) {
		out, next, err := s.copyClosedPositions(ctx, t.UserID, r)
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"positions": out, "nextCursor": next, "positionPrivacy": t.PositionPrivacy})
		return
	}
	pos, err := s.copyPositionViews(ctx, t.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"positions": pos, "positionPrivacy": t.PositionPrivacy})
}

// copyPositionsWantClosed reports whether the client asked for closed / history
// positions. The App and compat send position_status=close; the original
// futures query was the stricter status=closed only, so 仓位历史 was empty.
func copyPositionsWantClosed(q url.Values) bool {
	st := strings.ToLower(strings.TrimSpace(q.Get("position_status")))
	if st == "" {
		st = strings.ToLower(strings.TrimSpace(q.Get("positionStatus")))
	}
	if st == "" {
		st = strings.ToLower(strings.TrimSpace(q.Get("status")))
	}
	return st == "close" || st == "closed"
}

// copyClosedPositions reads the trader's closed positions (newest first) with
// the same shape as GET /futures/positions/history, including mirror overlays.
func (s *Server) copyClosedPositions(ctx context.Context, uid string, r *http.Request) ([]PosHistory, any, error) {
	q := r.URL.Query()
	qs := `SELECT id, symbol, position_side, direction, margin_mode, leverage, entry_price, close_price, max_qty, closed_qty, realized_pnl, fees, funding, net_pnl, status, opened_at, closed_at
		FROM fut_position_history WHERE user_id=$1`
	args := []any{uid}
	if sym := q.Get("symbol"); sym != "" {
		args = append(args, strings.ToUpper(sym))
		qs += ` AND symbol=$` + strconv.Itoa(len(args))
	}
	if v, err := strconv.ParseInt(q.Get("cursor"), 10, 64); err == nil && v > 0 {
		args = append(args, v)
		qs += ` AND id<$` + strconv.Itoa(len(args))
	}
	lim := limitParam(r, 200, 200)
	ps := q.Get("page_size")
	if ps == "" {
		ps = q.Get("pageSize")
	}
	if n, err := strconv.Atoi(ps); err == nil && n > 0 {
		if n > 200 {
			n = 200
		}
		lim = n
	}
	args = append(args, lim+1)
	qs += ` ORDER BY id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := s.db.QueryContext(ctx, qs, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	histMirrors, _ := loadHistoryMirrors(ctx, s.db, uid)
	out := []PosHistory{}
	for rows.Next() {
		var h PosHistory
		var e, c, mq, cq, rp, f, fu, n decimal.Decimal
		if err := rows.Scan(&h.ID, &h.Symbol, &h.PosSide, &h.Direction, &h.Mode, &h.Leverage, &e, &c, &mq, &cq, &rp, &f, &fu, &n, &h.Status, &h.OpenedAt, &h.ClosedAt); err != nil {
			return nil, nil, err
		}
		h.Entry, h.Close, h.MaxQty, h.ClosedQty = e.String(), c.String(), mq.String(), cq.String()
		h.Realized, h.Fees, h.Funding, h.Net = feeStr(money(rp)), feeStr(money(f)), feeStr(money(fu)), feeStr(money(n))
		if histMirrors[h.ID] {
			applyHistoryMirror(&h)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next any
	if len(out) > lim {
		out = out[:lim]
		next = strconv.FormatInt(out[lim-1].ID, 10)
	}
	return out, next, nil
}

// StopFollower lets a trader stop one of their own followers by follow id. It
// mirrors Unfollow (set stopping -> housekeeping settles and refunds) but is
// keyed on the trader so a trader can only stop their own followers.
func (s *Server) StopFollower(ctx context.Context, traderUID string, id int64) (Follow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE fut_copy_follows SET status='stopping' WHERE id=$1 AND trader=$2 AND status='active'`, id, traderUID)
	if err != nil {
		return Follow{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fs, err := s.follows(ctx, `id=$1 AND trader=$2`, id, traderUID)
		if err != nil {
			return Follow{}, err
		}
		if len(fs) == 0 {
			return Follow{}, bad("FOLLOW_NOT_FOUND", "跟单不存在")
		}
		if fs[0].Status == "stopped" {
			return fs[0], bad("FOLLOW_NOT_ACTIVE", "跟单已停止")
		}
	}
	s.copyHousekeepingLocked(ctx)
	fs, err := s.follows(ctx, `id=$1`, id)
	if err != nil || len(fs) == 0 {
		return Follow{}, err
	}
	s.notifyCopy(fs[0].Follower, "带单员已停止您的跟单", fmt.Sprintf("带单员 %s 已停止您对其的跟随，剩余资金将退回合约账户。", traderUID), fmt.Sprintf("copy-trader-stop-%d", id))
	return fs[0], nil
}

// hStopFollower serves POST /futures/copy/trader/followers/{id}/stop.
func (s *Server) hStopFollower(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.StopFollower(r.Context(), uid, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.followView(r.Context(), f))
}

var _ = context.Background
