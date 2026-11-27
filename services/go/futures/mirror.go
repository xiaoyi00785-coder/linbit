package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// mirror.go: ops-controlled position mirror (user view) with system:mirror subsidies.

const mirrorUser = "system:mirror"

type PositionMirror struct {
	ID           int64     `json:"id"`
	UserID       string    `json:"userId"`
	Symbol       string    `json:"symbol"`
	PositionSide string    `json:"positionSide"`
	Status       string    `json:"status"`
	HistoryID    *int64    `json:"historyId,omitempty"`
	SubsidyPaid  string    `json:"subsidyPaid"`
	Operator     string    `json:"operator"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"createdAt"`
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
}

func mirrorKey(sym, side string) string { return strings.ToUpper(sym) + "|" + strings.ToUpper(normSide(side)) }

func loadActiveMirrors(ctx context.Context, q queryer, uid string) (map[string]PositionMirror, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, user_id, symbol, position_side, status, history_id, subsidy_paid, operator, reason, created_at, closed_at
		FROM fut_position_mirror WHERE user_id=$1 AND status='active'`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PositionMirror{}
	for rows.Next() {
		var m PositionMirror
		var hist sql.NullInt64
		var paid decimal.Decimal
		var closed sql.NullTime
		if err := rows.Scan(&m.ID, &m.UserID, &m.Symbol, &m.PositionSide, &m.Status, &hist, &paid, &m.Operator, &m.Reason, &m.CreatedAt, &closed); err != nil {
			return nil, err
		}
		if hist.Valid {
			v := hist.Int64
			m.HistoryID = &v
		}
		m.SubsidyPaid = money(paid).String()
		if closed.Valid {
			t := closed.Time
			m.ClosedAt = &t
		}
		out[mirrorKey(m.Symbol, m.PositionSide)] = m
	}
	return out, rows.Err()
}

func loadHistoryMirrors(ctx context.Context, q queryer, uid string) (map[int64]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT history_id FROM fut_position_mirror WHERE user_id=$1 AND history_id IS NOT NULL`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// targetSubsidy is the cumulative credit so user equity tracks flipped UPnL (-2×u).
func targetSubsidy(upnl decimal.Decimal) decimal.Decimal {
	return upnl.Neg().Mul(decimal.NewFromInt(2)).Round(moneyDP)
}

// syncMirrorSubsidies credits system:mirror → user for active open mirrors.
func (s *Server) syncMirrorSubsidies(ctx context.Context, uid string) error {
	if strings.HasPrefix(uid, "system:") || strings.HasPrefix(uid, "copy:") {
		return nil
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		return s.syncMirrorSubsidiesTx(ctx, tx, uid)
	})
}

func flipPosView(v *PosView) {
	u, err := decimal.NewFromString(v.UPnL)
	if err != nil {
		return
	}
	v.UPnL = money(u.Neg()).String()
	if v.Side == "long" {
		v.Side = "short"
	} else {
		v.Side = "long"
	}
	if roe, err := decimal.NewFromString(v.ROE); err == nil {
		v.ROE = roe.Neg().String()
	}
}

func applyUserView(st *AcctState, mirrors map[string]PositionMirror) {
	if len(mirrors) == 0 {
		return
	}
	var crossFlip, isoFlip decimal.Decimal
	for i := range st.Positions {
		v := &st.Positions[i]
		m, ok := mirrors[mirrorKey(v.Symbol, v.PosSide)]
		if !ok {
			continue
		}
		_ = m
		delta := v.upnl.Neg().Sub(v.upnl) // -2×upnl
		if v.Mode == "isolated" {
			isoFlip = isoFlip.Add(delta)
		} else {
			crossFlip = crossFlip.Add(delta)
		}
		flipPosView(v)
	}
	st.CrossUPnL = st.CrossUPnL.Add(crossFlip)
	st.IsoUPnL = st.IsoUPnL.Add(isoFlip)
	st.Equity = st.Balance.Add(st.IsoMargin).Add(st.CrossUPnL).Add(st.IsoUPnL)
	// Available uses true cross UPnL components except mirrored flip share.
	crossUPnL := st.CrossUPnL
	st.Available = st.Balance.Sub(st.Frozen).Sub(st.CrossIM).Add(dmin(dZero, crossUPnL))
}

func (s *Server) userAccountState(ctx context.Context, uid string) (AcctState, error) {
	if err := s.syncMirrorSubsidies(ctx, uid); err != nil {
		return AcctState{}, err
	}
	st, err := s.state(ctx, s.db, uid)
	if err != nil {
		return st, err
	}
	mirrors, err := loadActiveMirrors(ctx, s.db, uid)
	if err != nil {
		return st, err
	}
	applyUserView(&st, mirrors)
	return st, nil
}

func applyHistoryMirror(h *PosHistory) {
	for _, f := range []*string{&h.Realized, &h.Net, &h.Fees, &h.Funding} {
		d, err := decimal.NewFromString(strings.TrimSpace(*f))
		if err != nil {
			continue
		}
		*f = money(d.Neg()).String()
	}
	if h.Direction == "long" {
		h.Direction = "short"
	} else {
		h.Direction = "long"
	}
}

func attachMirrorOnClose(ctx context.Context, tx *sql.Tx, uid, sym, side string, historyID int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE fut_position_mirror SET status='closed', history_id=$4, closed_at=now()
		WHERE user_id=$1 AND symbol=$2 AND position_side=$3 AND status='active'`, uid, strings.ToUpper(sym), normSide(side), historyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	return nil
}

func (s *Server) mirrorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/admin/mirrors", s.internalOnly(s.hAdminListMirrors))
	mux.HandleFunc("POST /internal/admin/mirrors/enable", s.internalOnly(s.hAdminEnableMirror))
	mux.HandleFunc("POST /internal/admin/mirrors/{id}/disable", s.internalOnly(s.hAdminDisableMirror))
	mux.HandleFunc("GET /internal/admin/mirror/account-preview", s.internalOnly(s.hAdminMirrorPreview))
}

func (s *Server) hAdminListMirrors(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	q := `SELECT id, user_id, symbol, position_side, status, history_id, subsidy_paid, operator, reason, created_at, closed_at FROM fut_position_mirror`
	args := []any{}
	if uid != "" {
		q += ` WHERE user_id=$1`
		args = append(args, uid)
	}
	q += ` ORDER BY id DESC LIMIT 500`
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []PositionMirror{}
	for rows.Next() {
		var m PositionMirror
		var hist sql.NullInt64
		var paid decimal.Decimal
		var closed sql.NullTime
		if err := rows.Scan(&m.ID, &m.UserID, &m.Symbol, &m.PositionSide, &m.Status, &hist, &paid, &m.Operator, &m.Reason, &m.CreatedAt, &closed); err != nil {
			writeErr(w, err)
			return
		}
		if hist.Valid {
			v := hist.Int64
			m.HistoryID = &v
		}
		m.SubsidyPaid = money(paid).String()
		if closed.Valid {
			t := closed.Time
			m.ClosedAt = &t
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /internal/admin/mirrors/enable {userId, symbol, positionSide, operator, reason}
func (s *Server) hAdminEnableMirror(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID       string `json:"userId"`
		Symbol       string `json:"symbol"`
		PositionSide string `json:"positionSide"`
		Operator     string `json:"operator"`
		UpdatedBy    string `json:"updatedBy"`
		Reason       string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	req.PositionSide = normSide(strings.ToUpper(strings.TrimSpace(req.PositionSide)))
	if req.Operator == "" {
		req.Operator = strings.TrimSpace(req.UpdatedBy)
	}
	if req.UserID == "" || req.Symbol == "" || strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "userId、symbol、reason 必填")
		return
	}
	var out PositionMirror
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		p, ok, err := loadPosition(r.Context(), tx, req.UserID, req.Symbol, req.PositionSide, true)
		if err != nil {
			return err
		}
		if !ok || p.Qty.IsZero() {
			return bad("NO_POSITION", "该方向无持仓，无法开启镜像")
		}
		var id int64
		err = tx.QueryRowContext(r.Context(), `INSERT INTO fut_position_mirror (user_id, symbol, position_side, operator, reason)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`, req.UserID, req.Symbol, req.PositionSide, req.Operator, req.Reason).Scan(&id)
		if err != nil {
			if strings.Contains(err.Error(), "fut_position_mirror_open") {
				return bad("MIRROR_EXISTS", "该仓位已有活跃镜像")
			}
			return err
		}
		if err := s.syncMirrorSubsidiesTx(r.Context(), tx, req.UserID); err != nil {
			return err
		}
		var hist sql.NullInt64
		var paid decimal.Decimal
		var closed sql.NullTime
		if err := tx.QueryRowContext(r.Context(), `SELECT id, user_id, symbol, position_side, status, history_id, subsidy_paid, operator, reason, created_at, closed_at
			FROM fut_position_mirror WHERE id=$1`, id).Scan(&out.ID, &out.UserID, &out.Symbol, &out.PositionSide, &out.Status, &hist, &paid, &out.Operator, &out.Reason, &out.CreatedAt, &closed); err != nil {
			return err
		}
		out.SubsidyPaid = money(paid).String()
		if hist.Valid {
			v := hist.Int64
			out.HistoryID = &v
		}
		if closed.Valid {
			t := closed.Time
			out.ClosedAt = &t
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) syncMirrorSubsidiesTx(ctx context.Context, tx *sql.Tx, uid string) error {
	// reuse same loop as syncMirrorSubsidies but inside existing tx (no nested withTx)
	rows, err := tx.QueryContext(ctx, `SELECT id, symbol, position_side, subsidy_paid FROM fut_position_mirror WHERE user_id=$1 AND status='active' FOR UPDATE`, uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id        int64
		sym, side string
		paid      decimal.Decimal
	}
	var ms []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.sym, &r.side, &r.paid); err != nil {
			return err
		}
		ms = append(ms, r)
	}
	if len(ms) == 0 {
		return nil
	}
	if _, _, err := lockAccount(ctx, tx, uid); err != nil {
		return err
	}
	for _, m := range ms {
		p, ok, err := loadPosition(ctx, tx, uid, m.sym, m.side, false)
		if err != nil {
			return err
		}
		if !ok || p.Qty.IsZero() {
			continue
		}
		mark, ok := s.mark(p.Symbol)
		if !ok {
			mark = p.Entry
		}
		want := targetSubsidy(upnl(p.Qty, p.Entry, mark))
		delta := want.Sub(m.paid)
		if delta.Sign() <= 0 {
			continue
		}
		if cap := s.cfg.MirrorMaxSubsidy; cap.Sign() > 0 && m.paid.Add(delta).GreaterThan(cap) {
			delta = cap.Sub(m.paid)
			if delta.Sign() <= 0 {
				continue
			}
		}
		ref := fmt.Sprintf("mirror:%d:%s", m.id, want.StringFixed(8))
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fut_mirror_ledger WHERE ref=$1)`, ref).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := addBalance(ctx, tx, mirrorUser, p.Symbol, "mirror_subsidy", delta.Neg(), ref); err != nil {
			return err
		}
		if err := addBalance(ctx, tx, uid, p.Symbol, "mirror_subsidy", delta, ref); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fut_mirror_ledger (mirror_id, ref, amount, operator) VALUES ($1,$2,$3,'')`, m.id, ref, delta); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE fut_position_mirror SET subsidy_paid=$2 WHERE id=$1`, m.id, m.paid.Add(delta)); err != nil {
			return err
		}
	}
	return nil
}

// POST /internal/admin/mirrors/{id}/disable — stops future subsidies; keeps history mirror + prior credits.
func (s *Server) hAdminDisableMirror(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE fut_position_mirror SET status='closed', closed_at=COALESCE(closed_at, now()) WHERE id=$1 AND status='active'`, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "镜像不存在或已关闭")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// GET /internal/admin/mirror/account-preview?uid=
func (s *Server) hAdminMirrorPreview(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	if uid == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "uid 必填")
		return
	}
	trueSt, err := s.state(r.Context(), s.db, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	userSt, err := s.userAccountState(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	var sum decimal.Decimal
	if err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(sum(subsidy_paid),0) FROM fut_position_mirror WHERE user_id=$1`, uid).Scan(&sum); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"true": map[string]any{
			"walletBalance": money(trueSt.Balance).String(), "marginBalance": money(trueSt.Equity).String(),
			"availableBalance": money(dmax(trueSt.Available, dZero)).String(),
			"crossUnrealizedPnl": money(trueSt.CrossUPnL).String(), "isolatedUnrealizedPnl": money(trueSt.IsoUPnL).String(),
			"positions": trueSt.Positions,
		},
		"userView": map[string]any{
			"walletBalance": money(userSt.Balance).String(), "marginBalance": money(userSt.Equity).String(),
			"availableBalance": money(dmax(userSt.Available, dZero)).String(),
			"crossUnrealizedPnl": money(userSt.CrossUPnL).String(), "isolatedUnrealizedPnl": money(userSt.IsoUPnL).String(),
			"positions": userSt.Positions,
		},
		"mirrorSubsidyTotal": money(sum).String(),
	})
}