package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func (s *Server) partnerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /partner/rules", s.handlePartnerRules)
	mux.HandleFunc("POST /partner/apply", s.user(s.handlePartnerApply))
	mux.HandleFunc("GET /partner/me", s.user(s.handlePartnerMe))
	mux.HandleFunc("GET /partner/summary", s.user(s.partnerOnly(s.handlePartnerSummary)))
	mux.HandleFunc("GET /partner/commissions", s.user(s.partnerOnly(s.handlePartnerCommissions)))
	mux.HandleFunc("GET /partner/commissions/daily", s.user(s.partnerOnly(s.handlePartnerDaily)))
	mux.HandleFunc("GET /partner/team", s.user(s.partnerOnly(s.handlePartnerTeam)))
	mux.HandleFunc("GET /partner/links", s.user(s.handleLinks))
	mux.HandleFunc("GET /internal/partners", s.internal(s.handleInternalPartners))
	mux.HandleFunc("POST /internal/partners/{uid}/review", s.internal(s.handleInternalPartnerReview))
}

func (s *Server) handlePartnerRules(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"tiers":           s.cfg.Tiers,
		"tierBasis":       "直推用户近" + strconv.Itoa(int(s.cfg.TierWindow.Hours()/24)) + "日合约交易额",
		"overrideRate":    s.cfg.OverrideRate.String(),
		"overrideLevels":  s.cfg.OverrideDepth + 1, // total levels incl. direct
		"overrideDepth":   s.cfg.OverrideDepth,     // extra override levels
		"overrideFunding": "平台出资，不扣减下级合伙人佣金",
		"settlement":      "每日结算至资金账户",
		"settleHourUtc":   s.cfg.SettleHourUTC,
		"payoutAsset":     s.cfg.PayoutAsset,
		"validity":        "终身有效",
		"rebateBase":      "实际支付手续费（不含体验金及手续费抵扣券抵扣部分）",
		"autoApprove":     s.cfg.PartnerAutoOK,
	})
}

type partnerRow struct {
	UID        string          `json:"uid"`
	Status     string          `json:"status"`
	Tier       string          `json:"tier"`
	Contact    string          `json:"contact"`
	Channel    string          `json:"channel"`
	Scale      string          `json:"scale"`
	ReviewNote string          `json:"reviewNote"`
	ReviewedBy string          `json:"reviewedBy,omitempty"`
	Volume30d  decimal.Decimal `json:"volume30d"`
	AppliedAt  int64           `json:"appliedAt"`
	ApprovedAt *int64          `json:"approvedAt"`
}

const partnerCols = `uid, status, tier, contact, channel, scale, review_note, reviewed_by, volume30d, applied_at, approved_at`

func scanPartner(sc interface{ Scan(...any) error }) (*partnerRow, error) {
	var p partnerRow
	var applied time.Time
	var approved sql.NullTime
	if err := sc.Scan(&p.UID, &p.Status, &p.Tier, &p.Contact, &p.Channel, &p.Scale, &p.ReviewNote, &p.ReviewedBy, &p.Volume30d, &applied, &approved); err != nil {
		return nil, err
	}
	p.AppliedAt = applied.UnixMilli()
	if approved.Valid {
		ms := approved.Time.UnixMilli()
		p.ApprovedAt = &ms
	}
	return &p, nil
}

func (s *Server) getPartner(ctx context.Context, uid string) (*partnerRow, error) {
	p, err := scanPartner(s.db.QueryRowContext(ctx, `SELECT `+partnerCols+` FROM g_partners WHERE uid=$1`, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Server) partnerOnly(next func(http.ResponseWriter, *http.Request, string)) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, uid string) {
		p, err := s.getPartner(r.Context(), uid)
		if err != nil {
			writeErr(w, err)
			return
		}
		if p == nil || p.Status != "active" {
			common.WriteError(w, http.StatusForbidden, "NOT_PARTNER", "您还不是合伙人")
			return
		}
		next(w, r, uid)
	}
}

func clean(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= max
}

func (s *Server) handlePartnerApply(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct{ Contact, Channel, Scale string }
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	contact, ok1 := clean(req.Contact, 64)
	channel, ok2 := clean(req.Channel, 64)
	scale, ok3 := clean(req.Scale, 64)
	if contact == "" || !ok1 || !ok2 || !ok3 {
		writeErr(w, bad("INVALID_APPLICATION", "请填写联系方式（各字段不超过64个字符）"))
		return
	}
	ctx := r.Context()
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	status := "pending"
	var approved any
	if s.cfg.PartnerAutoOK {
		status, approved = "active", time.Now()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO g_partners (uid, status, contact, channel, scale, approved_at) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (uid) DO UPDATE SET status=$2, contact=$3, channel=$4, scale=$5, approved_at=$6, review_note='', reviewed_by='', applied_at=now()
		WHERE g_partners.status IN ('rejected')`, uid, status, contact, channel, scale, approved)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, conflict("PARTNER_EXISTS", "已提交申请或已是合伙人"))
		return
	}
	p, _ := s.getPartner(ctx, uid)
	common.WriteJSON(w, http.StatusOK, p)
}

func (s *Server) handlePartnerMe(w http.ResponseWriter, r *http.Request, uid string) {
	p, err := s.getPartner(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if p == nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"uid": uid, "status": "none"})
		return
	}
	t := s.cfg.tier(p.Tier)
	common.WriteJSON(w, http.StatusOK, map[string]any{"partner": p, "uid": uid, "status": p.Status, "tier": t.Key, "tierName": t.Name,
		"futuresRate": t.FuturesRate, "spotRate": t.SpotRate})
}

// liveVolume is the directs' futures notional over the trailing window.
func (s *Server) liveVolume(ctx context.Context, uid string) decimal.Decimal {
	var v decimal.Decimal
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(e.notional),0) FROM g_fee_events e JOIN g_users u ON u.uid=e.uid
		WHERE u.inviter=$1 AND e.product='futures' AND e.at >= $2`, uid, s.now().Add(-s.cfg.TierWindow)).Scan(&v)
	return v
}

func (s *Server) sumComm(ctx context.Context, uid, where string, args ...any) decimal.Decimal {
	var v decimal.Decimal
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_commissions WHERE beneficiary=$1 `+where, append([]any{uid}, args...)...).Scan(&v)
	return trunc(v, 8)
}

func (s *Server) handlePartnerSummary(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	p, _ := s.getPartner(ctx, uid)
	t := s.cfg.tier(p.Tier)
	vol := s.liveVolume(ctx, uid)
	var next map[string]any
	for _, x := range s.cfg.Tiers {
		if x.MinVolume.GreaterThan(vol) && x.MinVolume.GreaterThan(t.MinVolume) {
			next = map[string]any{"tier": x.Key, "name": x.Name, "minVolume30d": x.MinVolume, "gap": x.MinVolume.Sub(vol)}
			break
		}
	}
	var directs, directPartners int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(p.uid) FROM g_users u LEFT JOIN g_partners p ON p.uid=u.uid AND p.status='active'
		WHERE u.inviter=$1`, uid).Scan(&directs, &directPartners)
	est := decimal.Zero
	if rows, err := s.computeCommissions(ctx, s.db, s.now(), uid); err == nil {
		for _, c := range rows {
			est = est.Add(c.Amount)
		}
	}
	now := s.now()
	today := s.day(now)
	month := today[:7] + "-01"
	var pending decimal.Decimal
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM g_payouts WHERE uid=$1 AND status='pending'`, uid).Scan(&pending)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"tier": t.Key, "tierName": t.Name, "futuresRate": t.FuturesRate, "spotRate": t.SpotRate,
		"volume30d": vol, "nextTier": next,
		"directs": directs, "directPartners": directPartners,
		"unsettledEstimate": trunc(est, 8),
		"yesterday":         s.sumComm(ctx, uid, `AND day=$2`, s.day(now.Add(-24*time.Hour))),
		"month":             s.sumComm(ctx, uid, `AND day >= $2`, month),
		"total":             s.sumComm(ctx, uid, ``),
		"overrideTotal":     s.sumComm(ctx, uid, `AND kind='override'`),
		"pendingPayout":     pending,
		"payoutAsset":       s.cfg.PayoutAsset,
	})
}

func (s *Server) listComm(w http.ResponseWriter, r *http.Request, uid string, kinds []string) {
	page, size := pageArgs(r)
	ctx := r.Context()
	if k := r.URL.Query().Get("kind"); k != "" {
		ok := false
		for _, x := range kinds {
			if x == k {
				ok = true
			}
		}
		if !ok {
			writeErr(w, bad("INVALID_KIND", "无效的佣金类型"))
			return
		}
		kinds = []string{k}
	}
	var total int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM g_commissions WHERE beneficiary=$1 AND kind = ANY($2)`, uid, kinds).Scan(&total)
	rows, err := s.db.QueryContext(ctx, `SELECT c.settlement_id, c.source_uid, c.kind, c.tier, c.rate, c.base, c.amount, to_char(c.day,'YYYY-MM-DD'), st.created_at
		FROM g_commissions c JOIN g_settlements st ON st.id=c.settlement_id
		WHERE c.beneficiary=$1 AND c.kind = ANY($2) ORDER BY c.day DESC, c.id DESC LIMIT $3 OFFSET $4`, uid, kinds, size, (page-1)*size)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var sid int64
		var src, kind, tier, day string
		var rate, base, amt decimal.Decimal
		var at time.Time
		if err := rows.Scan(&sid, &src, &kind, &tier, &rate, &base, &amt, &day, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"settlementId": sid, "sourceUid": src, "kind": kind, "tier": tier, "rate": rate,
			"base": base.Round(8), "amount": trunc(amt, 8), "day": day, "settledAt": at.UnixMilli(), "asset": s.cfg.PayoutAsset})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (s *Server) handlePartnerCommissions(w http.ResponseWriter, r *http.Request, uid string) {
	s.listComm(w, r, uid, []string{"futures", "spot", "override", "referral"})
}

func (s *Server) handlePartnerDaily(w http.ResponseWriter, r *http.Request, uid string) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 90 {
		days = 30
	}
	from := s.day(s.now().Add(-time.Duration(days-1) * 24 * time.Hour))
	rows, err := s.db.QueryContext(r.Context(), `SELECT to_char(day,'YYYY-MM-DD'), kind, SUM(amount) FROM g_commissions
		WHERE beneficiary=$1 AND day >= $2 GROUP BY day, kind ORDER BY day`, uid, from)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	byDay := map[string]map[string]any{}
	var order []string
	for rows.Next() {
		var day, kind string
		var amt decimal.Decimal
		if err := rows.Scan(&day, &kind, &amt); err != nil {
			writeErr(w, err)
			return
		}
		d := byDay[day]
		if d == nil {
			d = map[string]any{"day": day, "futures": decimal.Zero, "spot": decimal.Zero, "override": decimal.Zero, "referral": decimal.Zero, "total": decimal.Zero}
			byDay[day] = d
			order = append(order, day)
		}
		d[kind] = trunc(amt, 8)
		d["total"] = d["total"].(decimal.Decimal).Add(trunc(amt, 8))
	}
	items := make([]map[string]any, 0, len(order))
	for _, d := range order {
		items = append(items, byDay[d])
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "days": days})
}

func (s *Server) handlePartnerTeam(w http.ResponseWriter, r *http.Request, uid string) {
	page, size := pageArgs(r)
	ctx := r.Context()
	filter := ""
	switch r.URL.Query().Get("type") {
	case "partner":
		filter = " AND p.uid IS NOT NULL"
	case "user":
		filter = " AND p.uid IS NULL"
	}
	var total int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM g_users u LEFT JOIN g_partners p ON p.uid=u.uid AND p.status='active' WHERE u.inviter=$1`+filter, uid).Scan(&total)
	since := s.now().Add(-s.cfg.TierWindow)
	rows, err := s.db.QueryContext(ctx, `SELECT u.uid, u.registered_at, u.kyc_level, p.uid IS NOT NULL, COALESCE(p.tier,''),
		COALESCE((SELECT SUM(notional) FROM g_fee_events e WHERE e.uid=u.uid AND e.product='futures' AND e.at >= $2),0),
		COALESCE((SELECT SUM(notional) FROM g_fee_events e WHERE e.uid=u.uid AND e.product='spot' AND e.at >= $2),0),
		COALESCE((SELECT SUM(fee) FROM g_fee_events e WHERE e.uid=u.uid AND e.at >= $2),0),
		COALESCE((SELECT SUM(amount) FROM g_commissions c WHERE c.beneficiary=$1 AND c.source_uid=u.uid),0)
		FROM g_users u LEFT JOIN g_partners p ON p.uid=u.uid AND p.status='active'
		WHERE u.inviter=$1`+filter+` ORDER BY u.registered_at DESC LIMIT $3 OFFSET $4`, uid, since, size, (page-1)*size)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var u, tier string
		var reg time.Time
		var kyc int
		var isP bool
		var fv, sv, fee, comm decimal.Decimal
		if err := rows.Scan(&u, &reg, &kyc, &isP, &tier, &fv, &sv, &fee, &comm); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"uid": u, "joinedAt": reg.UnixMilli(), "kycLevel": kyc, "isPartner": isP, "tier": tier,
			"futuresVolume30d": fv, "spotVolume30d": sv, "fee30d": trunc(fee, 8), "commission": trunc(comm, 8)})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (s *Server) inviteCode(ctx context.Context, uid string) (string, error) {
	if err := s.ensureUser(ctx, uid); err != nil {
		return "", err
	}
	var code string
	err := s.db.QueryRowContext(ctx, `SELECT invite_code FROM g_users WHERE uid=$1`, uid).Scan(&code)
	return code, err
}

func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request, uid string) {
	code, err := s.inviteCode(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"code": code, "url": s.cfg.InviteBaseURL + code,
		"registerUrl": strings.TrimSuffix(s.cfg.InviteBaseURL, "r/") + "register?inviteCode=" + code})
}

// --- internal review (called by the admin service) ---

func (s *Server) handleInternalPartners(w http.ResponseWriter, r *http.Request) {
	page, size := pageArgs(r)
	st := r.URL.Query().Get("status")
	where, args := "", []any{size, (page - 1) * size}
	if st != "" {
		where, args = " WHERE status=$3", append(args, st)
	}
	var total int
	if st != "" {
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM g_partners WHERE status=$1`, st).Scan(&total)
	} else {
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM g_partners`).Scan(&total)
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+partnerCols+` FROM g_partners`+where+` ORDER BY applied_at DESC LIMIT $1 OFFSET $2`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*partnerRow{}
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (s *Server) handleInternalPartnerReview(w http.ResponseWriter, r *http.Request) {
	var req struct{ Decision, Reason, Reviewer string }
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	uid := r.PathValue("uid")
	reason, _ := clean(req.Reason, 200)
	var q string
	switch req.Decision {
	case "approve":
		q = `UPDATE g_partners SET status='active', approved_at=now(), review_note=$2, reviewed_by=$3 WHERE uid=$1 AND status='pending'`
	case "reject":
		if reason == "" {
			writeErr(w, bad("REASON_REQUIRED", "驳回需填写原因"))
			return
		}
		q = `UPDATE g_partners SET status='rejected', review_note=$2, reviewed_by=$3 WHERE uid=$1 AND status='pending'`
	case "disable":
		if reason == "" {
			writeErr(w, bad("REASON_REQUIRED", "停用需填写原因"))
			return
		}
		q = `UPDATE g_partners SET status='disabled', review_note=$2, reviewed_by=$3 WHERE uid=$1 AND status='active'`
	default:
		writeErr(w, bad("INVALID_DECISION", "无效的审核结果"))
		return
	}
	res, err := s.db.ExecContext(r.Context(), q, uid, reason, req.Reviewer)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		p, _ := s.getPartner(r.Context(), uid)
		if p == nil {
			writeErr(w, notFound("APPLICATION_NOT_FOUND", "申请不存在"))
		} else {
			writeErr(w, conflict("NOT_PENDING", "当前状态不可执行该操作"))
		}
		return
	}
	p, _ := s.getPartner(r.Context(), uid)
	common.WriteJSON(w, http.StatusOK, p)
}
