package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// ---- coupons ----

type couponReq struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // cash | fee_discount | trial
	FaceValue string `json:"faceValue"`
	Quantity  int    `json:"quantity"`
	MinSpend  string `json:"minSpend"`
	ValidDays int    `json:"validDays"`
	PerUser   int    `json:"perUser"`
}

type couponDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	FaceValue  string `json:"faceValue"`
	Quantity   int    `json:"quantity"`
	Budget     string `json:"budget"`
	MinSpend   string `json:"minSpend"`
	ValidDays  int    `json:"validDays"`
	PerUser    int    `json:"perUser"`
	Status     string `json:"status"`
	CreatedBy  int64  `json:"createdBy"`
	ReviewedBy *int64 `json:"reviewedBy,omitempty"`
	ReviewNote string `json:"reviewNote,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

const couponCols = `id, name, type, face_value::text, quantity, budget::text, min_spend::text, valid_days, per_user, status, created_by, reviewed_by, COALESCE(review_note,''), created_at`

func scanCoupon(sc interface{ Scan(...interface{}) error }) (*couponDTO, error) {
	var c couponDTO
	var rb sql.NullInt64
	var at time.Time
	if err := sc.Scan(&c.ID, &c.Name, &c.Type, &c.FaceValue, &c.Quantity, &c.Budget, &c.MinSpend, &c.ValidDays, &c.PerUser, &c.Status, &c.CreatedBy, &rb, &c.ReviewNote, &at); err != nil {
		return nil, err
	}
	if rb.Valid {
		c.ReviewedBy = &rb.Int64
	}
	c.CreatedAt = fmtTime(at)
	for _, p := range []*string{&c.FaceValue, &c.Budget, &c.MinSpend} {
		if d, err := decimal.NewFromString(*p); err == nil {
			*p = d.String()
		}
	}
	return &c, nil
}

var couponTypes = map[string]bool{"cash": true, "fee_discount": true, "trial": true}

func (s *Server) handleCreateCoupon(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req couponReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	fv, err := decimal.NewFromString(req.FaceValue)
	minSpend := decimal.Zero
	if req.MinSpend != "" {
		minSpend, _ = decimal.NewFromString(req.MinSpend)
	}
	if req.PerUser == 0 {
		req.PerUser = 1
	}
	if strings.TrimSpace(req.Name) == "" || !couponTypes[req.Type] || err != nil || fv.Sign() <= 0 || req.Quantity <= 0 || req.ValidDays <= 0 || minSpend.IsNegative() || req.PerUser < 1 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "优惠券参数不合法")
		return
	}
	budget := fv.Mul(decimal.NewFromInt(int64(req.Quantity)))
	status := "active"
	if budget.GreaterThan(s.cfg.CouponBudgetThreshold) {
		status = "pending_finance"
	}
	c, err := scanCoupon(s.db.QueryRowContext(r.Context(), `INSERT INTO coupon_templates (name, type, face_value, quantity, budget, min_spend, valid_days, per_user, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+couponCols,
		req.Name, req.Type, fv.String(), req.Quantity, budget.String(), minSpend.String(), req.ValidDays, req.PerUser, status, a.ID))
	if err != nil {
		internalErr(w, "create coupon", err)
		return
	}
	lvl := levelSensitive
	if status == "pending_finance" {
		lvl = levelHigh
	}
	s.audit(r, a, "coupon", "create", "coupon:"+strconv.FormatInt(c.ID, 10), lvl, status, nil, c, "")
	common.WriteJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListCoupons(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + couponCols + ` FROM coupon_templates`
	var args []interface{}
	if st := r.URL.Query().Get("status"); st != "" {
		q += ` WHERE status = $1`
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY id DESC LIMIT 200`, args...)
	if err != nil {
		internalErr(w, "list coupons", err)
		return
	}
	defer rows.Close()
	items := []*couponDTO{}
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			internalErr(w, "scan coupon", err)
			return
		}
		items = append(items, c)
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleCouponDecision(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id, ok := pathID(r)
		if !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
			return
		}
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		note := firstNonEmpty(req.Comment, req.Reason)
		if decision == "reject" && note == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
			return
		}
		before, err := scanCoupon(s.db.QueryRowContext(r.Context(), `SELECT `+couponCols+` FROM coupon_templates WHERE id = $1`, id))
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "优惠券不存在")
			return
		} else if err != nil {
			internalErr(w, "coupon load", err)
			return
		}
		if before.CreatedBy == a.ID {
			common.WriteError(w, http.StatusForbidden, "SELF_APPROVAL", "创建人不能审批自己的优惠券")
			return
		}
		status := "active"
		if decision == "reject" {
			status = "rejected"
		}
		after, err := scanCoupon(s.db.QueryRowContext(r.Context(), `UPDATE coupon_templates SET status = $2, reviewed_by = $3, review_note = $4, updated_at = now()
			WHERE id = $1 AND status = 'pending_finance' RETURNING `+couponCols, id, status, a.ID, nullStr(note)))
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该优惠券不在待审批状态")
			return
		} else if err != nil {
			internalErr(w, "coupon decision", err)
			return
		}
		s.audit(r, a, "coupon", decision, "coupon:"+strconv.FormatInt(id, 10), levelHigh, "ok", before, after, note)
		common.WriteJSON(w, http.StatusOK, after)
	}
}

// ---- rewards ----

type rewardReq struct {
	UserID string `json:"userId"`
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
	Reason string `json:"reason"`
}

type rewardDTO struct {
	ID         int64  `json:"id"`
	UserID     string `json:"userId"`
	Asset      string `json:"asset"`
	Amount     string `json:"amount"`
	USDTValue  string `json:"usdtValue,omitempty"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
	CreatedBy  int64  `json:"createdBy"`
	ReviewedBy *int64 `json:"reviewedBy,omitempty"`
	ReviewNote string `json:"reviewNote,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"createdAt"`
	ExecutedAt string `json:"executedAt,omitempty"`
}

const rewardCols = `id, user_id, asset, amount::text, COALESCE(usdt_value::text,''), reason, status, created_by, reviewed_by, COALESCE(review_note,''), COALESCE(error,''), created_at, executed_at`

func scanReward(sc interface{ Scan(...interface{}) error }) (*rewardDTO, error) {
	var d rewardDTO
	var rb sql.NullInt64
	var at time.Time
	var ex sql.NullTime
	if err := sc.Scan(&d.ID, &d.UserID, &d.Asset, &d.Amount, &d.USDTValue, &d.Reason, &d.Status, &d.CreatedBy, &rb, &d.ReviewNote, &d.Error, &at, &ex); err != nil {
		return nil, err
	}
	if rb.Valid {
		d.ReviewedBy = &rb.Int64
	}
	d.CreatedAt = fmtTime(at)
	if ex.Valid {
		d.ExecutedAt = fmtTime(ex.Time)
	}
	for _, p := range []*string{&d.Amount, &d.USDTValue} {
		if v, err := decimal.NewFromString(*p); err == nil {
			*p = v.String()
		}
	}
	return &d, nil
}

func (s *Server) getReward(ctx context.Context, id int64) (*rewardDTO, error) {
	return scanReward(s.db.QueryRowContext(ctx, `SELECT `+rewardCols+` FROM reward_grants WHERE id = $1`, id))
}

// executeReward credits the user through asset's idempotent /internal/reward.
func (s *Server) executeReward(ctx context.Context, d *rewardDTO) error {
	u, err := s.call(ctx, http.MethodPost, s.cfg.AssetURL, "/internal/reward", map[string]string{
		"userId": d.UserID, "asset": d.Asset, "amount": d.Amount, "ref": "admin_reward_" + strconv.FormatInt(d.ID, 10),
	})
	if err == nil && !u.ok() {
		err = errors.New("asset " + strconv.Itoa(u.Status) + " " + u.errCode())
	}
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE reward_grants SET status = 'failed', error = $2 WHERE id = $1`, d.ID, err.Error())
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE reward_grants SET status = 'executed', error = NULL, executed_at = now() WHERE id = $1`, d.ID)
	return err
}

// handleCreateReward: at or below the threshold executes immediately; above it
// (or when the asset has no USDT price) waits for finance_second review.
func (s *Server) handleCreateReward(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req rewardReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	amt, err := decimal.NewFromString(req.Amount)
	if req.UserID == "" || req.Asset == "" || err != nil || amt.Sign() <= 0 || strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "userId/asset/amount/reason 必填且金额为正")
		return
	}
	val, priced := s.usdtValue(r.Context(), req.Asset, amt)
	status := "executing"
	if !priced || val.GreaterThan(s.cfg.RewardReviewThreshold) {
		status = "pending_finance"
	}
	var usdt interface{}
	if priced {
		usdt = val.String()
	}
	d, err := scanReward(s.db.QueryRowContext(r.Context(), `INSERT INTO reward_grants (user_id, asset, amount, usdt_value, reason, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+rewardCols, req.UserID, req.Asset, amt.String(), usdt, req.Reason, status, a.ID))
	if err != nil {
		internalErr(w, "create reward", err)
		return
	}
	code := http.StatusCreated
	if status == "executing" {
		if err := s.executeReward(r.Context(), d); err != nil {
			code = http.StatusBadGateway
		}
		d, _ = s.getReward(r.Context(), d.ID)
	}
	lvl := levelSensitive
	if status == "pending_finance" {
		lvl = levelHigh
	}
	s.audit(r, a, "reward", "create", "reward:"+strconv.FormatInt(d.ID, 10), lvl, d.Status, nil, d, req.Reason)
	common.WriteJSON(w, code, d)
}

func (s *Server) handleListRewards(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + rewardCols + ` FROM reward_grants`
	var args []interface{}
	if st := r.URL.Query().Get("status"); st != "" {
		q += ` WHERE status = $1`
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY id DESC LIMIT 200`, args...)
	if err != nil {
		internalErr(w, "list rewards", err)
		return
	}
	defer rows.Close()
	items := []*rewardDTO{}
	for rows.Next() {
		d, err := scanReward(rows)
		if err != nil {
			internalErr(w, "scan reward", err)
			return
		}
		items = append(items, d)
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleRewardDecision(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id, ok := pathID(r)
		if !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
			return
		}
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		note := firstNonEmpty(req.Comment, req.Reason)
		if decision == "reject" && note == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
			return
		}
		before, err := s.getReward(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "奖励不存在")
			return
		} else if err != nil {
			internalErr(w, "reward load", err)
			return
		}
		if before.CreatedBy == a.ID {
			common.WriteError(w, http.StatusForbidden, "SELF_APPROVAL", "发起人不能审批自己的奖励")
			return
		}
		if decision == "approve" && !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		status := "executing"
		if decision == "reject" {
			status = "rejected"
		}
		res, err := s.db.ExecContext(r.Context(), `UPDATE reward_grants SET status = $2, reviewed_by = $3, review_note = $4 WHERE id = $1 AND status = 'pending_finance'`,
			id, status, a.ID, nullStr(note))
		if err != nil {
			internalErr(w, "reward decision", err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该奖励不在待审核状态")
			return
		}
		code := http.StatusOK
		if decision == "approve" {
			d, _ := s.getReward(r.Context(), id)
			if err := s.executeReward(r.Context(), d); err != nil {
				code = http.StatusBadGateway
			}
		}
		after, _ := s.getReward(r.Context(), id)
		s.audit(r, a, "reward", decision, "reward:"+strconv.FormatInt(id, 10), levelHigh, after.Status, before, after, note)
		common.WriteJSON(w, code, after)
	}
}

// ---- announcements ----

type announcementDTO struct {
	ID        int64  `json:"id"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	PublishAt string `json:"publishAt"`
	CreatedBy string `json:"createdBy,omitempty"`
}

func (s *Server) queryAnnouncements(ctx context.Context, publishedOnly bool) ([]announcementDTO, error) {
	q := `SELECT id, category, title, body, publish_at, COALESCE(created_by,'') FROM announcements`
	var args []interface{}
	if publishedOnly {
		q += ` WHERE publish_at <= $1`
		args = append(args, s.now())
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY publish_at DESC, id DESC LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []announcementDTO{}
	for rows.Next() {
		var d announcementDTO
		var at time.Time
		if err := rows.Scan(&d.ID, &d.Category, &d.Title, &d.Body, &at, &d.CreatedBy); err != nil {
			return nil, err
		}
		d.PublishAt = fmtTime(at)
		if publishedOnly {
			d.CreatedBy = ""
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Server) handlePublicAnnouncements(w http.ResponseWriter, r *http.Request) {
	items, err := s.queryAnnouncements(r.Context(), true)
	if err != nil {
		internalErr(w, "announcements", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleListAnnouncements(w http.ResponseWriter, r *http.Request) {
	items, err := s.queryAnnouncements(r.Context(), false)
	if err != nil {
		internalErr(w, "announcements", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleCreateAnnouncement(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Category  string `json:"category"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		PublishAt string `json:"publishAt"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Body) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "标题和内容必填")
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	at := s.now()
	if req.PublishAt != "" {
		t, err := time.Parse(time.RFC3339, req.PublishAt)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "publishAt 需为 RFC3339")
			return
		}
		at = t
	}
	var id int64
	if err := s.db.QueryRowContext(r.Context(), `INSERT INTO announcements (category, title, body, publish_at, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		req.Category, req.Title, req.Body, at, a.Username).Scan(&id); err != nil {
		internalErr(w, "create announcement", err)
		return
	}
	d := announcementDTO{ID: id, Category: req.Category, Title: req.Title, Body: req.Body, PublishAt: fmtTime(at), CreatedBy: a.Username}
	s.audit(r, a, "content", "create_announcement", "announcement:"+strconv.FormatInt(id, 10), levelNormal, "ok", nil, d, "")
	common.WriteJSON(w, http.StatusCreated, d)
}

// ---- audit log query ----

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, size := pageParams(r)
	where := []string{}
	args := []interface{}{}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, strings.Replace(cond, "?", "$"+strconv.Itoa(len(args)), 1))
	}
	qv := r.URL.Query()
	if v := qv.Get("adminId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			add("admin_id = ?", id)
		}
	}
	for _, k := range []string{"module", "action", "level", "result"} {
		if v := qv.Get(k); v != "" {
			add(k+" = ?", v)
		}
	}
	if v := qv.Get("target"); v != "" {
		add("target = ?", v)
	}
	if v := qv.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("at >= ?", t)
		}
	}
	if v := qv.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("at < ?", t)
		}
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM audit_logs`+cond, args...).Scan(&total); err != nil {
		internalErr(w, "audit count", err)
		return
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, at, admin_id, COALESCE(admin_name,''), roles, module, action, COALESCE(target,''), COALESCE(ip,''),
		COALESCE(user_agent,''), COALESCE(request_id,''), level, result, COALESCE(before::text,''), COALESCE(after::text,''), COALESCE(reason,'')
		FROM audit_logs`+cond+` ORDER BY id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		internalErr(w, "audit query", err)
		return
	}
	defer rows.Close()
	type item struct {
		ID        int64       `json:"id"`
		At        string      `json:"at"`
		AdminID   *int64      `json:"adminId,omitempty"`
		Admin     string      `json:"admin"`
		Roles     []string    `json:"roles"`
		Module    string      `json:"module"`
		Action    string      `json:"action"`
		Target    string      `json:"target,omitempty"`
		IP        string      `json:"ip,omitempty"`
		UserAgent string      `json:"userAgent,omitempty"`
		RequestID string      `json:"requestId,omitempty"`
		Level     string      `json:"level"`
		Result    string      `json:"result"`
		Before    interface{} `json:"before,omitempty"`
		After     interface{} `json:"after,omitempty"`
		Reason    string      `json:"reason,omitempty"`
	}
	items := []item{}
	for rows.Next() {
		var it item
		var at time.Time
		var aid sql.NullInt64
		var before, after string
		if err := rows.Scan(&it.ID, &at, &aid, &it.Admin, (*textArray)(&it.Roles), &it.Module, &it.Action, &it.Target, &it.IP, &it.UserAgent, &it.RequestID,
			&it.Level, &it.Result, &before, &after, &it.Reason); err != nil {
			internalErr(w, "audit scan", err)
			return
		}
		it.At = fmtTime(at)
		if aid.Valid {
			it.AdminID = &aid.Int64
		}
		if before != "" {
			it.Before = rawJSON(before)
		}
		if after != "" {
			it.After = rawJSON(after)
		}
		items = append(items, it)
	}
	s.audit(r, actorOf(r), "audit", "query", "", levelNormal, "ok", nil, nil, "")
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total, "page": page, "pageSize": size})
}

type rawJSON string

func (j rawJSON) MarshalJSON() ([]byte, error) { return []byte(j), nil }
