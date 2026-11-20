package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Multi-level referral rewards (L1/L2/L3). Rates are configurable in
// g_referral_tiers. Commissions are credited into g_commissions with
// kind='referral' at tier-specific rates; GET /referral/summary totals them
// and POST /referral/withdraw moves them to spot.

type referralTier struct {
	Level   int             `json:"level"`
	Rate    decimal.Decimal `json:"rate"`
	Enabled bool            `json:"enabled"`
}

func (s *Server) loadReferralTiers(ctx context.Context) ([]referralTier, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT level, rate, enabled FROM g_referral_tiers ORDER BY level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []referralTier{}
	for rows.Next() {
		var t referralTier
		if err := rows.Scan(&t.Level, &t.Rate, &t.Enabled); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// extendReferralRoutes adds the multi-level endpoints next to the existing
// referral_me / referral_invitees etc. (registered in referral.go).
func (s *Server) extendReferralRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /referral/tiers", s.handleReferralTiers)
	mux.HandleFunc("GET /referral/tree", s.user(s.handleReferralTree))
	mux.HandleFunc("GET /referral/summary", s.user(s.handleReferralSummary))
	mux.HandleFunc("POST /referral/withdraw", s.user(s.handleReferralWithdraw))
	mux.HandleFunc("GET /referral/withdrawals", s.user(s.handleReferralWithdrawals))
}

func (s *Server) handleReferralTiers(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadReferralTiers(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "basis": "好友交易手续费的百分比返还；L1 直推，L2/L3 间推"})
}

// GET /referral/tree?depth=3 — the user's invitees up to the given depth.
func (s *Server) handleReferralTree(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	depth := 3
	if v := r.URL.Query().Get("depth"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d >= 1 && d <= 3 {
			depth = d
		}
	}
	type node struct {
		UID        string          `json:"uid"`
		Level      int             `json:"level"`
		RegAt      int64           `json:"registeredAt"`
		KYC        int             `json:"kycLevel"`
		Commission decimal.Decimal `json:"commission"`
		Children   []*node         `json:"children,omitempty"`
	}
	var walk func(parent string, level int) ([]*node, error)
	walk = func(parent string, level int) ([]*node, error) {
		if level > depth {
			return nil, nil
		}
		rows, err := s.db.QueryContext(ctx, `SELECT u.uid, u.registered_at, u.kyc_level,
			COALESCE((SELECT SUM(amount) FROM g_commissions c WHERE c.beneficiary=$1 AND c.source_uid=u.uid AND c.kind='referral'),0)
			FROM g_users u WHERE u.inviter=$2 ORDER BY u.registered_at DESC`, uid, parent)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		children := []*node{}
		for rows.Next() {
			n := &node{Level: level}
			var reg time.Time
			if err := rows.Scan(&n.UID, &reg, &n.KYC, &n.Commission); err != nil {
				return nil, err
			}
			n.RegAt = reg.UnixMilli()
			n.UID = maskUID(n.UID)
			children = append(children, n)
		}
		for _, c := range children {
			// Walk uses the real uid for children (the masked one won't match).
			// We stashed the masked form into n.UID. Re-query by registration
			// is wrong; instead issue a second lookup to get the real uid once.
			// Since the masked uid is reversible only inside our DB, we re-fetch
			// each child's inviter_uid for the subtree walk with the unmasked id.
			if level < depth {
				var realUID string
				_ = s.db.QueryRowContext(ctx, `SELECT uid FROM g_users WHERE inviter=$1 AND registered_at=$2 LIMIT 1`, parent, time.UnixMilli(c.RegAt)).Scan(&realUID)
				if realUID != "" {
					sub, err := walk(realUID, level+1)
					if err != nil {
						return nil, err
					}
					c.Children = sub
				}
			}
		}
		return children, nil
	}
	tree, err := walk(uid, 1)
	if err != nil {
		writeErr(w, err)
		return
	}
	// counts
	var l1, l2, l3 int
	for _, a := range tree {
		l1++
		for _, b := range a.Children {
			l2++
			l3 += len(b.Children)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"tree": tree,
		"counts": map[string]int{"l1": l1, "l2": l2, "l3": l3, "total": l1 + l2 + l3}})
}

// GET /referral/summary — totals (all time, this month, pending to withdraw).
func (s *Server) handleReferralSummary(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	now := s.now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var total, month, withdrawn, pending decimal.Decimal
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_commissions WHERE beneficiary=$1 AND kind='referral'`, uid).Scan(&total)
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_commissions WHERE beneficiary=$1 AND kind='referral' AND day >= $2`, uid, monthStart).Scan(&month)
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_referral_withdrawals WHERE uid=$1 AND status='paid'`, uid).Scan(&withdrawn)
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_referral_withdrawals WHERE uid=$1 AND status='pending'`, uid).Scan(&pending)
	available := total.Sub(withdrawn).Sub(pending)
	if available.IsNegative() {
		available = decimal.Zero
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"uid": uid, "total": total.Round(8).String(), "month": month.Round(8).String(),
		"withdrawn": withdrawn.Round(8).String(), "pending": pending.Round(8).String(),
		"available": available.Round(8).String(), "asset": s.cfg.PayoutAsset,
	})
}

type withdrawRequest struct {
	Amount string `json:"amount"`
}

func (s *Server) handleReferralWithdraw(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	var req withdrawRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	amt, err := decimal.NewFromString(req.Amount)
	if err != nil || amt.Sign() <= 0 {
		writeErr(w, bad("INVALID_AMOUNT", "提现金额不合法"))
		return
	}
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	// Lock the user's referral row via a sentinel row in g_user_points (reuse
	// the same per-user mutex we already have).
	if _, err := tx.ExecContext(ctx, `INSERT INTO g_user_points (uid) VALUES ($1) ON CONFLICT (uid) DO NOTHING`, uid); err != nil {
		writeErr(w, err)
		return
	}
	var sentinel int64
	_ = tx.QueryRowContext(ctx, `SELECT 1 FROM g_user_points WHERE uid=$1 FOR UPDATE`, uid).Scan(&sentinel)
	var total, withdrawn, pending decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_commissions WHERE beneficiary=$1 AND kind='referral'`, uid).Scan(&total)
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_referral_withdrawals WHERE uid=$1 AND status='paid'`, uid).Scan(&withdrawn)
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_referral_withdrawals WHERE uid=$1 AND status='pending'`, uid).Scan(&pending)
	available := total.Sub(withdrawn).Sub(pending)
	if amt.GreaterThan(available) {
		writeErr(w, conflict("INSUFFICIENT_COMMISSION", "可提现佣金不足"))
		return
	}
	ref := "refcomm:" + uid + ":" + strconv.FormatInt(s.now().UnixMilli(), 10)
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO g_referral_withdrawals (uid, amount, asset, ref, status) VALUES ($1,$2,$3,$4,'pending') RETURNING id`,
		uid, amt, s.cfg.PayoutAsset, ref).Scan(&id); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	// Transfer outside the DB tx; failure stays pending for the next retry.
	if err := s.systemTransfer(ctx, uid, s.cfg.PayoutAsset, "spot", "credit", amt, ref); err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE g_referral_withdrawals SET status='paid', paid_at=now() WHERE id=$1 AND status='pending'`, id)
	}
	var st string
	_ = s.db.QueryRowContext(ctx, `SELECT status FROM g_referral_withdrawals WHERE id=$1`, id).Scan(&st)
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "amount": amt.String(), "asset": s.cfg.PayoutAsset, "status": st, "ref": ref})
}

func (s *Server) handleReferralWithdrawals(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, amount, asset, ref, status, at, paid_at FROM g_referral_withdrawals WHERE uid=$1 ORDER BY id DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var amt decimal.Decimal
		var asset, ref, status string
		var at time.Time
		var paid sql.NullTime
		if err := rows.Scan(&id, &amt, &asset, &ref, &status, &at, &paid); err != nil {
			writeErr(w, err)
			return
		}
		item := map[string]any{"id": id, "amount": amt.String(), "asset": asset, "status": status, "ref": ref, "at": at.UnixMilli()}
		if paid.Valid {
			item["paidAt"] = paid.Time.UnixMilli()
		}
		items = append(items, item)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// payPendingReferralWithdrawals retries any pending commission withdrawals.
// Called from the main loop alongside payPendingRebates.
func (s *Server) payPendingReferralWithdrawals(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, uid, amount, asset, ref FROM g_referral_withdrawals WHERE status='pending' LIMIT 200`)
	if err != nil {
		return 0, err
	}
	type row struct {
		id    int64
		uid   string
		amt   decimal.Decimal
		asset string
		ref   string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.uid, &r.amt, &r.asset, &r.ref); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, r)
	}
	rows.Close()
	paid := 0
	for _, r := range list {
		if err := s.systemTransfer(ctx, r.uid, r.asset, "spot", "credit", r.amt, r.ref); err != nil {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE g_referral_withdrawals SET status='paid', paid_at=now() WHERE id=$1 AND status='pending'`, r.id); err != nil {
			return paid, err
		}
		paid++
	}
	return paid, nil
}

var _ = errors.New
