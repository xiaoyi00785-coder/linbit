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
