package main

import (
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Regular referral: the inviter earns ReferralRate of each invitee's paid
// fees for ReferralPeriod after the invitee registers; one level, never
// stacked with partner commission (an active partner earns partner rates).

func (s *Server) referralRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /referral/me", s.user(s.handleReferralMe))
	mux.HandleFunc("GET /referral/invitees", s.user(s.handleReferralInvitees))
	mux.HandleFunc("GET /referral/rewards", s.user(func(w http.ResponseWriter, r *http.Request, uid string) {
		s.listComm(w, r, uid, []string{"referral"})
	}))
	mux.HandleFunc("GET /internal/referrals/{uid}", s.internal(s.handleInternalReferral))
	// Dev: shift a user's registration time back (tests the referral window).
	mux.HandleFunc("POST /internal/dev/growth/backdate", s.dev(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UID  string `json:"uid"`
			Days int    `json:"days"`
		}
		if err := common.DecodeJSON(r, &req); err != nil || req.Days <= 0 {
			common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
			return
		}
		res, err := s.db.ExecContext(r.Context(), `UPDATE g_users SET registered_at = registered_at - make_interval(days => $2) WHERE uid=$1`, req.UID, req.Days)
		if err != nil {
			writeErr(w, err)
			return
		}
		n, _ := res.RowsAffected()
		common.WriteJSON(w, http.StatusOK, map[string]any{"updated": n})
	}))
}

// handleInternalReferral is the support / admin view of one user's invite
// relationships and commission totals.
func (s *Server) handleInternalReferral(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := r.PathValue("uid")
	var code string
	var inviter *string
	var reg time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT invite_code, inviter, registered_at FROM g_users WHERE uid=$1`, uid).Scan(&code, &inviter, &reg); err != nil {
		writeErr(w, notFound("USER_NOT_FOUND", "用户不存在"))
		return
	}
	var invited int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM g_users WHERE inviter=$1`, uid).Scan(&invited)
	totals := map[string]decimal.Decimal{}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, SUM(amount) FROM g_commissions WHERE beneficiary=$1 GROUP BY kind`, uid)
	if err == nil {
		for rows.Next() {
			var k string
			var v decimal.Decimal
			if rows.Scan(&k, &v) == nil {
				totals[k] = trunc(v, 8)
			}
		}
		rows.Close()
	}
	p, _ := s.getPartner(ctx, uid)
	common.WriteJSON(w, http.StatusOK, map[string]any{"uid": uid, "code": code, "inviter": inviter, "registeredAt": reg.UnixMilli(),
		"referralUntil": reg.Add(s.cfg.ReferralPeriod).UnixMilli(), "invited": invited, "partner": p, "commissionTotals": totals})
}

func (s *Server) handleReferralMe(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	code, err := s.inviteCode(ctx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	var invited, verified int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE kyc_level >= 1) FROM g_users WHERE inviter=$1`, uid).Scan(&invited, &verified)
	p, _ := s.getPartner(ctx, uid)
	status := "none"
	if p != nil {
		status = p.Status
	}
	var inviter *string
	_ = s.db.QueryRowContext(ctx, `SELECT inviter FROM g_users WHERE uid=$1`, uid).Scan(&inviter)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"code": code, "url": s.cfg.InviteBaseURL + code,
		"rate": s.cfg.ReferralRate, "validDays": int(s.cfg.ReferralPeriod.Hours() / 24),
		"invited": invited, "verified": verified,
		"totalRewards":  s.sumComm(ctx, uid, `AND kind='referral'`),
		"partnerStatus": status, "isPartner": status == "active", "invitedBy": inviter,
		"asset": s.cfg.PayoutAsset,
	})
}

func (s *Server) handleReferralInvitees(w http.ResponseWriter, r *http.Request, uid string) {
	page, size := pageArgs(r)
	ctx := r.Context()
	var total int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM g_users WHERE inviter=$1`, uid).Scan(&total)
	rows, err := s.db.QueryContext(ctx, `SELECT u.uid, u.registered_at,
		COALESCE((SELECT SUM(amount) FROM g_commissions c WHERE c.beneficiary=$1 AND c.source_uid=u.uid AND c.kind='referral'),0)
		FROM g_users u WHERE u.inviter=$1 ORDER BY u.registered_at DESC LIMIT $2 OFFSET $3`, uid, size, (page-1)*size)
	if err != nil {
		writeErr(w, err)
		return
	}
	type inv struct {
		uid string
		reg time.Time
		amt decimal.Decimal
	}
	var list []inv
	var uids []string
	for rows.Next() {
		var x inv
		if err := rows.Scan(&x.uid, &x.reg, &x.amt); err != nil {
			rows.Close()
			writeErr(w, err)
			return
		}
		list = append(list, x)
		uids = append(uids, x.uid)
	}
	rows.Close()
	s.refreshKYC(ctx, uids)
	items := []map[string]any{}
	for _, x := range list {
		var kyc int
		_ = s.db.QueryRowContext(ctx, `SELECT kyc_level FROM g_users WHERE uid=$1`, x.uid).Scan(&kyc)
		until := x.reg.Add(s.cfg.ReferralPeriod)
		items = append(items, map[string]any{"uid": maskUID(x.uid), "registeredAt": x.reg.UnixMilli(), "kycLevel": kyc,
			"rewardUntil": until.UnixMilli(), "active": s.now().Before(until), "rewards": trunc(x.amt, 8)})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func maskUID(u string) string {
	if len(u) <= 4 {
		return u
	}
	return u[:2] + "****" + u[len(u)-2:]
}
