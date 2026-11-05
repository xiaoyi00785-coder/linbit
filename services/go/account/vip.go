package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/linbit/platform/services/go/common"
)

// vip.go: VIP application + manual review. The UI's form submits the UID
// (implicit), a contact channel and the contact value. The published
// conditions (total assets >= 500,000 USDT or 30-day volume >= 50,000,000 USDT)
// are enforced server-side at submit (vip_eligibility.go): an ineligible
// applicant gets 403 VIP_NOT_ELIGIBLE, and when assets/volume cannot be
// determined the call fails closed with a retryable 503
// VIP_ELIGIBILITY_UNAVAILABLE. Operators still review eligible applications in
// 1-3 business days. VIP has no fee tiers: fees are uniform.

var vipChannels = map[string]bool{"telegram": true, "whatsapp": true, "wechat": true, "email": true, "phone": true}

// VIPConditions are the published eligibility thresholds (defaults; the
// enforced values come from VIP_MIN_TOTAL_ASSETS_USDT / VIP_MIN_VOLUME_30D_USDT
// and are what GET /vip/application reports, see Server.vipConditions).
var VIPConditions = map[string]string{"minTotalAssetsUSDT": "500000", "min30dVolumeUSDT": "50000000", "reviewDays": "1-3"}

var (
	errVIPOpen    = errors.New("vip application already open")
	errVIPAlready = errors.New("already vip")
)

type vipApp struct {
	ID         string     `json:"id"`
	UID        string     `json:"uid,omitempty"`
	Channel    string     `json:"channel"`
	Contact    string     `json:"contact"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason,omitempty"`
	Reviewer   string     `json:"reviewer,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
}

const vipCols = `a.id, u.uid, a.channel, a.contact, a.status, a.reason, a.reviewer, a.created_at, a.reviewed_at`

func scanVIP(sc interface{ Scan(...any) error }) (*vipApp, error) {
	var a vipApp
	var id int64
	var rv sql.NullTime
	if err := sc.Scan(&id, &a.UID, &a.Channel, &a.Contact, &a.Status, &a.Reason, &a.Reviewer, &a.CreatedAt, &rv); err != nil {
		return nil, err
	}
	a.ID = "vip_" + strconv.FormatInt(id, 10)
	if rv.Valid {
		t := rv.Time
		a.ReviewedAt = &t
	}
	return &a, nil
}

// LatestVIPApplication returns the user's most recent application (nil if none).
func (s *Store) LatestVIPApplication(ctx context.Context, userID int64) (*vipApp, error) {
	a, err := scanVIP(s.db.QueryRowContext(ctx, `SELECT `+vipCols+` FROM vip_applications a JOIN users u ON u.id=a.user_id
		WHERE a.user_id=$1 ORDER BY a.id DESC LIMIT 1`, userID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *Store) CreateVIPApplication(ctx context.Context, userID int64, channel, contact string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO vip_applications (user_id, channel, contact) VALUES ($1,$2,$3)`, userID, channel, contact)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		var st string
		_ = s.db.QueryRowContext(ctx, `SELECT status FROM vip_applications WHERE user_id=$1 AND status IN ('pending','approved') LIMIT 1`, userID).Scan(&st)
		if st == "approved" {
			return errVIPAlready
		}
		return errVIPOpen
	}
	return err
}

func (s *Store) ListVIPApplications(ctx context.Context, status string, limit, offset int) ([]*vipApp, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vip_applications WHERE ($1='' OR status=$1)`, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+vipCols+` FROM vip_applications a JOIN users u ON u.id=a.user_id
		WHERE ($1='' OR a.status=$1) ORDER BY a.id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*vipApp{}
	for rows.Next() {
		a, err := scanVIP(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// ReviewVIPApplication moves a pending application to approved/rejected and
// returns the owning user id. ok=false when it is not pending.
func (s *Store) ReviewVIPApplication(ctx context.Context, id int64, status, reason, reviewer string) (int64, bool, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx, `UPDATE vip_applications SET status=$2, reason=$3, reviewer=$4, reviewed_at=now()
		WHERE id=$1 AND status='pending' RETURNING user_id`, id, status, reason, reviewer).Scan(&uid)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return uid, err == nil, err
}

// GET /vip/application
func (s *Server) handleGetVIP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	a, err := s.store.LatestVIPApplication(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	status := "none"
	if a != nil {
		status = a.Status
		a.UID = ""
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"uid": u.UID, "vip": status == "approved", "status": status, "application": a,
		"conditions": s.vipConditions(), "channels": []string{"telegram", "whatsapp", "wechat", "email", "phone"},
	})
}

// POST /vip/applications {channel, contact}
//
// 400 VALIDATION_ERROR; 409 ALREADY_VIP / APPLICATION_PENDING; 403
// VIP_NOT_ELIGIBLE (assets and 30-day volume both below the thresholds);
// 503 VIP_ELIGIBILITY_UNAVAILABLE (retryable: the figures could not be
// determined, never approved on missing data).
func (s *Server) handleApplyVIP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
		return
	}
	var req struct{ Channel, Contact string }
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	ch := strings.ToLower(strings.TrimSpace(req.Channel))
	contact := strings.TrimSpace(req.Contact)
	if !vipChannels[ch] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不支持的联系方式")
		return
	}
	if contact == "" || len(contact) > 128 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请输入联系方式")
		return
	}
	// Existing VIP / open application first: those answers must not depend on
	// the upstream balance and volume services being reachable.
	prev, err := s.store.LatestVIPApplication(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if prev != nil && prev.Status == "approved" {
		common.WriteError(w, http.StatusConflict, "ALREADY_VIP", "您已是 VIP")
		return
	}
	if prev != nil && prev.Status == "pending" {
		common.WriteError(w, http.StatusConflict, "APPLICATION_PENDING", "已有审核中的申请")
		return
	}
	if !s.cfg.VIPEligibilitySkip {
		chk, err := s.checkVIPEligibility(r.Context(), u.UID)
		if err != nil {
			log.Printf("vip: eligibility %s: %v", u.UID, err)
			writeVIPUnavailable(w)
			return
		}
		if !chk.Eligible {
			s.writeVIPNotEligible(w, chk)
			return
		}
	}
	switch err := s.store.CreateVIPApplication(r.Context(), u.ID, ch, contact); {
	case errors.Is(err, errVIPAlready):
		common.WriteError(w, http.StatusConflict, "ALREADY_VIP", "您已是 VIP")
		return
	case errors.Is(err, errVIPOpen):
		common.WriteError(w, http.StatusConflict, "APPLICATION_PENDING", "已有审核中的申请")
		return
	case err != nil:
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.handleGetVIP(w, r)
}

// GET /internal/vip/applications?status=&page=&pageSize=
func (s *Server) handleInternalListVIP(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
		return
	}
	q := r.URL.Query()
	st := q.Get("status")
	if st == "all" {
		st = ""
	}
	if st != "" && st != "pending" && st != "approved" && st != "rejected" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的状态")
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	items, total, err := s.store.ListVIPApplications(r.Context(), st, size, (page-1)*size)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

// POST /internal/vip/applications/{id}/review {decision: approve|reject, reason, reviewer}
func (s *Server) handleInternalReviewVIP(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.PathValue("id"), "vip_"), 10, 64)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的申请编号")
		return
	}
	var req struct{ Decision, Reason, Reviewer string }
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	status := map[string]string{"approve": "approved", "reject": "rejected"}[req.Decision]
	reason := strings.TrimSpace(req.Reason)
	if status == "" || (status == "rejected" && reason == "") {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "审核参数无效")
		return
	}
	userID, ok, err := s.store.ReviewVIPApplication(r.Context(), id, status, reason, req.Reviewer)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !ok {
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该申请不在待审核状态")
		return
	}
	if u, err := s.store.UserByID(r.Context(), userID); err == nil {
		if status == "approved" {
			s.notifyAsync(u, common.NoticeSystem, "VIP 申请已通过", "恭喜您成为 Linbit VIP，专属客户经理将尽快与您联系。")
		} else {
			s.notifyAsync(u, common.NoticeSystem, "VIP 申请未通过", "您的 VIP 申请未通过审核，原因："+reason+"。满足条件后可重新申请。")
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"id": "vip_" + strconv.FormatInt(id, 10), "status": status})
}
