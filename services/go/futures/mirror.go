package main

import (
	"context"
	"database/sql"
	"errors"
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
	BookFlipped  bool      `json:"bookFlipped"`
	HistoryID    *int64    `json:"historyId,omitempty"`
	SubsidyPaid  string    `json:"subsidyPaid"`
	Operator     string    `json:"operator"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"createdAt"`
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
}

func mirrorDisplayFlip(m PositionMirror) bool {
	return m.Status == "active" && !m.BookFlipped
}

func flipTradeSide(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "buy":
		return "sell"
	case "sell":
		return "buy"
	default:
		return s
	}
}

// applyBookMirrorOrder flips order.side for user-facing lists while book_flipped mirror is active.
// Only orders strictly before mirror.created_at are flipped (facts in DB stay; display matches post-flip book).
func applyBookMirrorOrder(o *Order, mirrors map[string]PositionMirror) {
	if o == nil || len(mirrors) == 0 {
		return
	}
	sym := strings.ToUpper(o.Symbol)
	ps := normSide(o.PosSide)
	for _, m := range mirrors {
		if !m.BookFlipped || m.Status != "active" {
			continue
		}
		if strings.ToUpper(m.Symbol) != sym {
			continue
		}
		mps := normSide(m.PositionSide)
		if mps != "BOTH" && mps != ps {
			continue
		}
		if mps == "BOTH" && ps != "BOTH" {
			continue
		}
		if !o.CreatedAt.Before(m.CreatedAt) {
			continue
		}
		o.Side = flipTradeSide(o.Side)
		return
	}
}

func applyBookMirrorOrders(orders []Order, mirrors map[string]PositionMirror) {
	for i := range orders {
		applyBookMirrorOrder(&orders[i], mirrors)
	}
}

// bookFlipPosition negates qty (entry unchanged). UPnL sign flips with the book.
func bookFlipPosition(ctx context.Context, tx *sql.Tx, p *Position, mirrorID int64, operator string) (preSign int, err error) {
	if p.Qty.IsZero() {
		return 0, bad("NO_POSITION", "持仓数量为 0")
	}
	preSign = p.Qty.Sign()
	p.Qty = p.Qty.Neg()
	p.TP, p.SL = decimal.NullDecimal{}, decimal.NullDecimal{}
	if err := savePosition(ctx, tx, *p); err != nil {
		return 0, err
	}
	ref := fmt.Sprintf("mirror:book_flip:%d", mirrorID)
	if err := addBill(ctx, tx, p.UserID, p.Symbol, "book_flip", decimal.Zero, ref); err != nil {
		return 0, err
	}
	_ = operator
	return preSign, nil
}

// cascadeCopyBookFlip flips copy sub-accounts on the same symbol/side when aligned with the trader pre-flip sign.
func (s *Server) cascadeCopyBookFlip(ctx context.Context, tx *sql.Tx, trader, sym, posSide string, alignSign int, mirrorID int64) error {
	if !s.traders[trader] {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM fut_copy_follows WHERE trader=$1 AND status='active' FOR UPDATE`, trader)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var fid int64
		if err := rows.Scan(&fid); err != nil {
			return err
		}
		acct := copyAcct(fid)
		cp, ok, err := loadPosition(ctx, tx, acct, sym, posSide, true)
		if err != nil {
			return err
		}
		if !ok || cp.Qty.IsZero() || cp.Qty.Sign() != alignSign {
			continue
		}
		cp.Qty = cp.Qty.Neg()
		cp.TP, cp.SL = decimal.NullDecimal{}, decimal.NullDecimal{}
		if err := savePosition(ctx, tx, cp); err != nil {
			return err
		}
		ref := fmt.Sprintf("mirror:copy_book_flip:%d:%d", mirrorID, fid)
		if err := addBill(ctx, tx, acct, sym, "book_flip", decimal.Zero, ref); err != nil {
			return err
		}
	}
	return rows.Err()
}

func mirrorKey(sym, side string) string { return strings.ToUpper(sym) + "|" + strings.ToUpper(normSide(side)) }

func loadActiveMirrors(ctx context.Context, q queryer, uid string) (map[string]PositionMirror, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, user_id, symbol, position_side, status, COALESCE(book_flipped,false), history_id, subsidy_paid, operator, reason, created_at, closed_at
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
		if err := rows.Scan(&m.ID, &m.UserID, &m.Symbol, &m.PositionSide, &m.Status, &m.BookFlipped, &hist, &paid, &m.Operator, &m.Reason, &m.CreatedAt, &closed); err != nil {
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
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT h.id
		FROM fut_position_history h
		INNER JOIN fut_position_mirror m
		  ON m.user_id = h.user_id
		 AND upper(m.symbol) = upper(h.symbol)
		 AND upper(m.position_side) = upper(h.position_side)
		WHERE h.user_id = $1
		  AND h.closed_at >= m.created_at - interval '1 second'
		  AND NOT COALESCE(m.book_flipped, false)
		  AND (
		    m.history_id = h.id
		    OR (
		      m.history_id IS NULL
		      AND h.closed_at <= COALESCE(m.closed_at, h.closed_at) + interval '10 minutes'
		    )
		  )`, uid)
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

// mirrorUserNet is realised − fees + funding on the user (mirrored) view.
func mirrorUserNet(rp, fees, fund decimal.Decimal) decimal.Decimal {
	return money(rp.Neg().Sub(fees).Add(fund.Neg()))
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

func closeOrderSide(q decimal.Decimal) string {
	if q.Sign() > 0 {
		return "sell"
	}
	return "buy"
}

func reduceMatches(p Position, side string) bool {
	if p.Qty.IsZero() {
		return false
	}
	return (p.Qty.Sign() > 0) != (side == "buy")
}

// resolveMirrorReduce fixes positionSide/side when a mirrored user closes from the flipped UI.
func resolveMirrorReduce(ctx context.Context, q queryer, uid, sym, posSide, side string) (string, string) {
	p, ok, err := loadPosition(ctx, q, uid, sym, posSide, false)
	if err == nil && ok && reduceMatches(p, side) {
		return posSide, side
	}
	mirrors, err := loadActiveMirrors(ctx, q, uid)
	if err != nil || len(mirrors) == 0 {
		return posSide, side
	}
	sym = strings.ToUpper(sym)
	for _, m := range mirrors {
		if strings.ToUpper(m.Symbol) != sym {
			continue
		}
		ps := normSide(m.PositionSide)
		p2, ok2, err2 := loadPosition(ctx, q, uid, sym, ps, false)
		if err2 != nil || !ok2 || p2.Qty.IsZero() {
			continue
		}
		return ps, closeOrderSide(p2.Qty)
	}
	return posSide, side
}

func flipPosView(v *PosView) {
	v.Mirrored = true
	v.CloseSide = closeOrderSide(v.pos.Qty)
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

func mirrorActiveSubsidyPaid(ctx context.Context, q queryer, uid string) (decimal.Decimal, error) {
	var sum decimal.Decimal
	err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(subsidy_paid),0) FROM fut_position_mirror WHERE user_id=$1 AND status='active'`, uid).Scan(&sum)
	return money(sum), err
}

// crossUPnLUserView is cross UPnL after mirror display flip (true state + flip deltas).
func crossUPnLUserView(st AcctState, mirrors map[string]PositionMirror) decimal.Decimal {
	if len(mirrors) == 0 {
		return st.CrossUPnL
	}
	var flip decimal.Decimal
	for _, v := range st.Positions {
		if v.Mode != "cross" {
			continue
		}
		m, ok := mirrors[mirrorKey(v.Symbol, v.PosSide)]
		if !ok || !mirrorDisplayFlip(m) {
			continue
		}
		flip = flip.Add(v.upnl.Neg().Sub(v.upnl))
	}
	return st.CrossUPnL.Add(flip)
}

func positionMirrored(mirrors map[string]PositionMirror, sym, posSide string) bool {
	_, ok := mirrors[mirrorKey(sym, posSide)]
	return ok
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
		if m.BookFlipped {
			v.Mirrored = true
			continue
		}
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
	h.Mirrored = true
	rp, err := decimal.NewFromString(strings.TrimSpace(h.Realized))
	if err != nil {
		return
	}
	fees, _ := decimal.NewFromString(strings.TrimSpace(h.Fees))
	fund, _ := decimal.NewFromString(strings.TrimSpace(h.Funding))
	rp = money(rp.Neg())
	fundM := money(fund.Neg())
	h.Realized = feeStr(rp)
	h.Fees = feeStr(money(fees))
	h.Funding = feeStr(fundM)
	h.Net = feeStr(money(rp.Sub(fees).Add(fundM)))
	if h.Direction == "long" {
		h.Direction = "short"
	} else {
		h.Direction = "long"
	}
}

// settleMirrorClose aligns wallet delta with mirrorUserNet after true net was booked.
func settleMirrorClose(ctx context.Context, tx *sql.Tx, uid, sym string, historyID int64, rp, fees, fund decimal.Decimal) error {
	trueNet := money(rp.Sub(fees).Add(fund))
	userNet := mirrorUserNet(rp, fees, fund)
	adj := money(userNet.Sub(trueNet))
	if adj.IsZero() {
		return nil
	}
	ref := fmt.Sprintf("mirror:close:%d", historyID)
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fut_bills WHERE user_id=$1 AND ref=$2)`, uid, ref).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := addBalance(ctx, tx, mirrorUser, sym, "mirror_close_settlement", adj.Neg(), ref); err != nil {
		return err
	}
	return addBalance(ctx, tx, uid, sym, "mirror_close_settlement", adj, ref)
}

func attachMirrorOnClose(ctx context.Context, tx *sql.Tx, uid, sym, side string, historyID int64, rp, fees, fund decimal.Decimal) error {
	var id int64
	var bookFlipped bool
	err := tx.QueryRowContext(ctx, `UPDATE fut_position_mirror SET status='closed', history_id=$4, closed_at=now()
		WHERE user_id=$1 AND symbol=$2 AND position_side=$3 AND status='active' RETURNING id, COALESCE(book_flipped,false)`,
		uid, strings.ToUpper(sym), normSide(side), historyID).Scan(&id, &bookFlipped)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `UPDATE fut_position_mirror SET status='closed', history_id=$3, closed_at=now()
			WHERE id = (
				SELECT id FROM fut_position_mirror
				WHERE user_id=$1 AND upper(symbol)=upper($2) AND status='active'
				ORDER BY id DESC LIMIT 1
			) RETURNING id, COALESCE(book_flipped,false)`, uid, sym, historyID).Scan(&id, &bookFlipped)
		if err == sql.ErrNoRows {
			return nil
		}
	}
	if err != nil {
		return err
	}
	if bookFlipped {
		return nil
	}
	return settleMirrorClose(ctx, tx, uid, sym, historyID, rp, fees, fund)
}

func (s *Server) mirrorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/admin/mirrors", s.internalOnly(s.hAdminListMirrors))
	mux.HandleFunc("POST /internal/admin/mirrors/enable", s.internalOnly(s.hAdminEnableMirror))
	mux.HandleFunc("POST /internal/admin/mirrors/{id}/disable", s.internalOnly(s.hAdminDisableMirror))
	mux.HandleFunc("GET /internal/admin/mirror/account-preview", s.internalOnly(s.hAdminMirrorPreview))
}

func (s *Server) hAdminListMirrors(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	q := `SELECT id, user_id, symbol, position_side, status, COALESCE(book_flipped,false), history_id, subsidy_paid, operator, reason, created_at, closed_at FROM fut_position_mirror`
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
		if err := rows.Scan(&m.ID, &m.UserID, &m.Symbol, &m.PositionSide, &m.Status, &m.BookFlipped, &hist, &paid, &m.Operator, &m.Reason, &m.CreatedAt, &closed); err != nil {
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
		preSign := p.Qty.Sign()
		var id int64
		err = tx.QueryRowContext(r.Context(), `INSERT INTO fut_position_mirror (user_id, symbol, position_side, operator, reason, book_flipped)
			VALUES ($1,$2,$3,$4,$5,true) RETURNING id`, req.UserID, req.Symbol, req.PositionSide, req.Operator, req.Reason).Scan(&id)
		if err != nil {
			if strings.Contains(err.Error(), "fut_position_mirror_open") {
				return bad("MIRROR_EXISTS", "该仓位已有活跃镜像")
			}
			return err
		}
		if _, err := bookFlipPosition(r.Context(), tx, &p, id, req.Operator); err != nil {
			return err
		}
		if err := s.cascadeCopyBookFlip(r.Context(), tx, req.UserID, req.Symbol, req.PositionSide, preSign, id); err != nil {
			return err
		}
		var hist sql.NullInt64
		var paid decimal.Decimal
		var closed sql.NullTime
		if err := tx.QueryRowContext(r.Context(), `SELECT id, user_id, symbol, position_side, status, COALESCE(book_flipped,false), history_id, subsidy_paid, operator, reason, created_at, closed_at
			FROM fut_position_mirror WHERE id=$1`, id).Scan(&out.ID, &out.UserID, &out.Symbol, &out.PositionSide, &out.Status, &out.BookFlipped, &hist, &paid, &out.Operator, &out.Reason, &out.CreatedAt, &closed); err != nil {
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
	rows, err := tx.QueryContext(ctx, `SELECT id, symbol, position_side, subsidy_paid FROM fut_position_mirror WHERE user_id=$1 AND status='active' AND NOT COALESCE(book_flipped,false) FOR UPDATE`, uid)
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

// POST /internal/admin/mirrors/{id}/disable — book-flip mirrors: flip book back + copy cascade; display mirrors: stop subsidies only.
func (s *Server) hAdminDisableMirror(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	err = s.withTx(r.Context(), func(tx *sql.Tx) error {
		var uid, sym, side string
		var bookFlipped bool
		err := tx.QueryRowContext(r.Context(), `SELECT user_id, symbol, position_side, COALESCE(book_flipped,false)
			FROM fut_position_mirror WHERE id=$1 AND status='active' FOR UPDATE`, id).Scan(&uid, &sym, &side, &bookFlipped)
		if errors.Is(err, sql.ErrNoRows) {
			return bad("NOT_FOUND", "镜像不存在或已关闭")
		}
		if err != nil {
			return err
		}
		if bookFlipped {
			p, ok, err := loadPosition(r.Context(), tx, uid, sym, side, true)
			if err != nil {
				return err
			}
			if ok && !p.Qty.IsZero() {
				preSign := p.Qty.Sign()
				if _, err := bookFlipPosition(r.Context(), tx, &p, id, ""); err != nil {
					return err
				}
				if err := s.cascadeCopyBookFlip(r.Context(), tx, uid, sym, side, preSign, id); err != nil {
					return err
				}
			}
		}
		res, err := tx.ExecContext(r.Context(), `UPDATE fut_position_mirror SET status='closed', closed_at=COALESCE(closed_at, now()) WHERE id=$1 AND status='active'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return bad("NOT_FOUND", "镜像不存在或已关闭")
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
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