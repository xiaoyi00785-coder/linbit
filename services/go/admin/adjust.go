package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// adjust.go: manual balance adjustments (人工调账), mainnet-safe.
//
//   - POST /admin/v1/adjustments (fund:adjust): uid, asset, account
//     (spot|funding), signed amount (>0 credit, <0 debit), reason, totpCode,
//     optional ref (idempotency key; generated when absent). A fresh 2FA code
//     is ALWAYS required, even when ADMIN_REQUIRE_2FA=false.
//   - |amount| valued in USDT >= the threshold (admin setting
//     adjust_second_review_usdt, default ADMIN_ADJUST_SECOND_REVIEW_USDT=1000)
//     or unpriceable (fail closed) -> pending_approval; a DIFFERENT admin with
//     fund:adjust:approve approves (2FA) or rejects. Below it applies at once.
//   - Applied through asset POST /internal/admin/adjust (tx_type admin_adjust,
//     counterparty system:admin_adjust, idempotent on ref, debit never below
//     zero, one DB transaction). Reconciliation reports the net per asset as
//     adminAdjustNet / unbackedAdjust.
//   - Every step is audited (level high) with IP, reason and before/after.

func dbErr(w http.ResponseWriter, where string, err error) {
	log.Printf("%s: %v", where, err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

const adjustThresholdKey = "adjust_second_review_usdt"

func (s *Server) registerAdjustRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/adjustments", permFundAdjust, s.handleAdjustList)
	a("POST /admin/v1/adjustments", permFundAdjust, s.handleAdjustCreate)
	a("GET /admin/v1/adjustments/config", permFundAdjust, s.handleAdjustConfigGet)
	a("PUT /admin/v1/adjustments/config", permFundAdjustApprove, s.handleAdjustConfigPut)
	a("GET /admin/v1/adjustments/{id}", permFundAdjust, s.handleAdjustGet)
	a("POST /admin/v1/adjustments/{id}/approve", permFundAdjustApprove, s.handleAdjustDecision(true))
	a("POST /admin/v1/adjustments/{id}/reject", permFundAdjustApprove, s.handleAdjustDecision(false))
}

func (s *Server) adjustThreshold(ctx context.Context) decimal.Decimal {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM admin_settings WHERE key=$1`, adjustThresholdKey).Scan(&v); err == nil {
		if d, err := decimal.NewFromString(v); err == nil && d.IsPositive() {
			return d
		}
	}
	return s.cfg.AdjustSecondAmount
}

type adjustment struct {
	ID          int64   `json:"id"`
	Ref         string  `json:"ref"`
	UID         string  `json:"uid"`
	Asset       string  `json:"asset"`
	Account     string  `json:"account"`
	Amount      string  `json:"amount"`
	Direction   string  `json:"direction"`
	USDTValue   *string `json:"usdtValue"`
	NeedsSecond bool    `json:"needsSecond"`
	Status      string  `json:"status"`
	Reason      string  `json:"reason"`
	CreatedBy   int64   `json:"createdBy"`
	CreatedName string  `json:"createdByName"`
	CreatedIP   *string `json:"createdIp"`
	ReviewedBy  *int64  `json:"reviewedBy"`
	ReviewName  *string `json:"reviewedByName"`
	ReviewNote  *string `json:"reviewNote"`
	ReviewedAt  *string `json:"reviewedAt"`
	FreeBefore  *string `json:"freeBefore"`
	FreeAfter   *string `json:"freeAfter"`
	Error       *string `json:"error"`
	CreatedAt   string  `json:"createdAt"`
	AppliedAt   *string `json:"appliedAt"`
}

const adjustCols = `b.id, b.ref, b.user_id, b.asset, b.account, b.amount::text, b.usdt_value::text, b.needs_second, b.status, b.reason,
	b.created_by, cu.username, b.created_ip, b.reviewed_by, ru.username, b.review_note, b.reviewed_at, b.free_before::text, b.free_after::text,
	b.error, b.created_at, b.applied_at
	FROM balance_adjustments b JOIN admin_users cu ON cu.id=b.created_by LEFT JOIN admin_users ru ON ru.id=b.reviewed_by`

type rowScanner interface{ Scan(...any) error }

func scanAdjust(r rowScanner) (adjustment, error) {
	var x adjustment
	var usdt, ip, rname, note, fb, fa, e sql.NullString
	var rby sql.NullInt64
	var rat, aat sql.NullTime
	var cat time.Time
	if err := r.Scan(&x.ID, &x.Ref, &x.UID, &x.Asset, &x.Account, &x.Amount, &usdt, &x.NeedsSecond, &x.Status, &x.Reason,
		&x.CreatedBy, &x.CreatedName, &ip, &rby, &rname, &note, &rat, &fb, &fa, &e, &cat, &aat); err != nil {
		return x, err
	}
	ns := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		s := v.String
		return &s
	}
	nd := func(v sql.NullString) *string {
		p := ns(v)
		if p != nil {
			if d, err := decimal.NewFromString(*p); err == nil {
				s := d.String()
				p = &s
			}
		}
		return p
	}
	nt := func(v sql.NullTime) *string {
		if !v.Valid {
			return nil
		}
		s := fmtTime(v.Time)
		return &s
	}
	x.Amount = decimal.RequireFromString(x.Amount).String()
	x.Direction = "credit"
	if strings.HasPrefix(x.Amount, "-") {
		x.Direction = "debit"
	}
	x.USDTValue, x.CreatedIP, x.ReviewName, x.ReviewNote, x.FreeBefore, x.FreeAfter, x.Error = nd(usdt), ns(ip), ns(rname), ns(note), nd(fb), nd(fa), ns(e)
	if rby.Valid {
		id := rby.Int64
		x.ReviewedBy = &id
	}
	x.ReviewedAt, x.AppliedAt, x.CreatedAt = nt(rat), nt(aat), fmtTime(cat)
	return x, nil
}

func (s *Server) loadAdjust(ctx context.Context, id int64) (adjustment, error) {
	return scanAdjust(s.db.QueryRowContext(ctx, `SELECT `+adjustCols+` WHERE b.id=$1`, id))
}

func (s *Server) handleAdjustList(w http.ResponseWriter, r *http.Request) {
	page, size := pageParams(r)
	f := &filter{}
	f.eq(r, "status", "b.status")
	f.eq(r, "uid", "b.user_id")
	f.eq(r, "account", "b.account")
	if v := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset"))); v != "" {
		f.add("b.asset = ?", v)
	}
	switch r.URL.Query().Get("direction") {
	case "credit":
		f.where = append(f.where, "b.amount > 0")
	case "debit":
		f.where = append(f.where, "b.amount < 0")
	}
	if v := r.URL.Query().Get("createdBy"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.add("b.created_by = ?", n)
		}
	}
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM balance_adjustments b`+f.sql(), f.args...).Scan(&total); err != nil {
		dbErr(w, "adjust count", err)
		return
	}
	args := append(f.args, size, (page-1)*size)
	n := len(f.args)
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+adjustCols+f.sql()+` ORDER BY b.id DESC LIMIT $`+strconv.Itoa(n+1)+` OFFSET $`+strconv.Itoa(n+2), args...)
	if err != nil {
		dbErr(w, "adjust list", err)
		return
	}
	defer rows.Close()
	items := []adjustment{}
	for rows.Next() {
		x, err := scanAdjust(rows)
		if err != nil {
			dbErr(w, "adjust scan", err)
			return
		}
		items = append(items, x)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "size": size,
		"thresholdUsdt": s.adjustThreshold(r.Context()).String()})
}

func (s *Server) handleAdjustGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	x, err := s.loadAdjust(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "调账单不存在")
		return
	}
	if err != nil {
		dbErr(w, "adjust get", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, x)
}

// require2FA: adjustments always need a bound authenticator and a valid code.
func (s *Server) require2FA(w http.ResponseWriter, a *actor, code string) bool {
	if a == nil || a.TOTP == "" {
		common.WriteError(w, http.StatusForbidden, "TWO_FACTOR_REQUIRED", "账号未绑定 2FA，不能调账")
		return false
	}
	return s.verify2FA(w, a, code)
}

func validAdjustUID(uid string) bool {
	return uid != "" && len(uid) <= 64 && !strings.HasPrefix(uid, "system:") && !strings.HasPrefix(uid, "mm:") && !strings.HasPrefix(uid, "platform:")
}

func (s *Server) handleAdjustCreate(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		UID      string `json:"uid"`
		Asset    string `json:"asset"`
		Account  string `json:"account"`
		Amount   string `json:"amount"`
		Ref      string `json:"ref"`
		Reason   string `json:"reason"`
		TOTPCode string `json:"totpCode"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	req.UID, req.Ref, req.Reason = strings.TrimSpace(req.UID), strings.TrimSpace(req.Ref), strings.TrimSpace(req.Reason)
	req.Asset, req.Account = strings.ToUpper(strings.TrimSpace(req.Asset)), strings.ToLower(strings.TrimSpace(req.Account))
	if req.Account == "" {
		req.Account = "spot"
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	switch {
	case !validAdjustUID(req.UID) || req.UID == s.cfg.MMUserID:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "uid 不合法（不能调整系统/做市账户）")
		return
	case req.Asset == "" || len(req.Asset) > 16:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "asset 不合法")
		return
	case req.Account != "spot" && req.Account != "funding":
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "account 仅支持 spot / funding")
		return
	case err != nil || amt.Sign() == 0 || amt.Exponent() < -8 || amt.Abs().GreaterThan(decimal.New(1, 18)):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "amount 不合法（非零，最多 8 位小数；负数为扣减）")
		return
	case len(req.Ref) > 100:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "ref 最长 100")
		return
	case req.Reason == "" || len(req.Reason) > 500:
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写调账原因（≤500 字）")
		return
	}
	if !s.require2FA(w, a, req.TOTPCode) {
		return
	}
	// Idempotent create on ref.
	if req.Ref != "" {
		var id int64
		var pu, pa, pacct, pamt string
		err := s.db.QueryRowContext(r.Context(), `SELECT id, user_id, asset, account, amount::text FROM balance_adjustments WHERE ref=$1`, req.Ref).Scan(&id, &pu, &pa, &pacct, &pamt)
		if err == nil {
			if pu != req.UID || pa != req.Asset || pacct != req.Account || !decimal.RequireFromString(pamt).Equal(amt) {
				common.WriteError(w, http.StatusConflict, "REF_CONFLICT", "ref 已被使用且参数不同")
				return
			}
			x, err := s.loadAdjust(r.Context(), id)
			if err != nil {
				dbErr(w, "adjust replay", err)
				return
			}
			if x.Status == "applying" && !x.NeedsSecond {
				out, code := s.applyAdjust(r, a, id, "adjust_apply_retry", req.Reason)
				common.WriteJSON(w, code, map[string]any{"adjustment": out, "replayed": true})
				return
			}
			common.WriteJSON(w, http.StatusOK, map[string]any{"adjustment": x, "replayed": true})
			return
		} else if !errors.Is(err, sql.ErrNoRows) {
			dbErr(w, "adjust ref", err)
			return
		}
	}
	uu, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/users/"+url.PathEscape(req.UID), nil)
	if err != nil {
		upstreamErr(w, "adjust user lookup", err)
		return
	}
	if !uu.ok() {
		relay(w, uu)
		return
	}
	usdt, priced := s.usdtValue(r.Context(), req.Asset, amt.Abs())
	threshold := s.adjustThreshold(r.Context())
	needs := !priced || usdt.GreaterThanOrEqual(threshold)
	var usdtArg any
	if priced {
		usdtArg = usdt.Round(8).String()
	}
	status := "applying"
	if needs {
		status = "pending_approval"
	}
	ref := req.Ref
	if ref == "" {
		ref = "adj-" + common.RequestID()
	}
	var id int64
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO balance_adjustments (ref, user_id, asset, account, amount, usdt_value, needs_second, status, reason, created_by, created_ip)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, ref, req.UID, req.Asset, req.Account, amt.String(), usdtArg, needs, status, req.Reason, a.ID, s.clientIP(r)).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "balance_adjustments_ref_key") {
			common.WriteError(w, http.StatusConflict, "REF_CONFLICT", "ref 已被使用")
			return
		}
		dbErr(w, "adjust insert", err)
		return
	}
	target := "user:" + req.UID
	params := map[string]any{"id": id, "ref": ref, "asset": req.Asset, "account": req.Account, "amount": amt.String(),
		"usdtValue": usdtArg, "thresholdUsdt": threshold.String(), "needsSecond": needs}
	if needs {
		s.audit(r, a, "ledger", "adjust_request", target, levelHigh, "ok", nil, params, req.Reason)
		x, _ := s.loadAdjust(r.Context(), id)
		common.WriteJSON(w, http.StatusCreated, map[string]any{"adjustment": x})
		return
	}
	x, code := s.applyAdjust(r, a, id, "adjust_apply", req.Reason)
	common.WriteJSON(w, code, map[string]any{"adjustment": x})
}

// applyAdjust calls asset for a row already in status "applying" and records
// the outcome. A transport error puts the row back so it can be retried (the
// asset ref makes a retry a replay).
func (s *Server) applyAdjust(r *http.Request, a *actor, id int64, action, reason string) (adjustment, int) {
	ctx := r.Context()
	x, err := s.loadAdjust(ctx, id)
	if err != nil {
		return x, http.StatusInternalServerError
	}
	revert := "applying"
	if x.NeedsSecond {
		revert = "pending_approval"
	}
	u, err := s.call(ctx, http.MethodPost, s.cfg.AssetURL, "/internal/admin/adjust",
		map[string]string{"userId": x.UID, "asset": x.Asset, "account": x.Account, "amount": x.Amount, "ref": x.Ref})
	target := "user:" + x.UID
	if err != nil {
		msg := "asset 不可用：" + err.Error()
		if x.NeedsSecond {
			_, _ = s.db.ExecContext(ctx, `UPDATE balance_adjustments SET status=$2, reviewed_by=NULL, reviewed_at=NULL, reviewed_ip=NULL, error=$3 WHERE id=$1`, id, revert, msg)
		} else {
			// Outcome unknown: stay "applying"; re-POST with the same ref retries
			// (asset replays if it was in fact applied).
			_, _ = s.db.ExecContext(ctx, `UPDATE balance_adjustments SET error=$2 WHERE id=$1`, id, msg)
		}
		s.audit(r, a, "ledger", action, target, levelHigh, "failed:UPSTREAM_UNAVAILABLE", nil, map[string]any{"id": id, "ref": x.Ref}, reason)
		x, _ = s.loadAdjust(ctx, id)
		return x, http.StatusBadGateway
	}
	if !u.ok() {
		code := u.errCode()
		_, _ = s.db.ExecContext(ctx, `UPDATE balance_adjustments SET status='failed', error=$2 WHERE id=$1`, id, code)
		s.audit(r, a, "ledger", action, target, levelHigh, "failed:"+code, nil, map[string]any{"id": id, "ref": x.Ref, "amount": x.Amount}, reason)
		x, _ = s.loadAdjust(ctx, id)
		st := u.Status
		if st < 400 || st >= 500 {
			st = http.StatusBadGateway
		}
		return x, st
	}
	var res struct {
		Before   string `json:"freeBefore"`
		After    string `json:"freeAfter"`
		Replayed bool   `json:"replayed"`
	}
	_ = json.Unmarshal(u.Body, &res)
	var fb, fa any
	if res.Before != "" {
		fb = res.Before
	}
	if res.After != "" {
		fa = res.After
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE balance_adjustments SET status='applied', applied_at=now(), error=NULL,
		free_before=COALESCE($2::numeric, free_before), free_after=COALESCE($3::numeric, free_after) WHERE id=$1`, id, fb, fa)
	before := map[string]any{"account": x.Account, "asset": x.Asset, "free": res.Before}
	after := map[string]any{"account": x.Account, "asset": x.Asset, "free": res.After, "amount": x.Amount, "ref": x.Ref,
		"txType": "admin_adjust", "counterparty": "system:admin_adjust", "id": id, "replayed": res.Replayed}
	s.audit(r, a, "ledger", action, target, levelHigh, "ok", before, after, reason)
	x, _ = s.loadAdjust(ctx, id)
	return x, http.StatusOK
}

func (s *Server) handleAdjustDecision(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id, ok := pathID(r)
		if !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
			return
		}
		var req struct {
			Note     string `json:"note"`
			Reason   string `json:"reason"`
			TOTPCode string `json:"totpCode"`
		}
		if r.ContentLength != 0 {
			if err := common.DecodeJSON(r, &req); err != nil {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
				return
			}
		}
		note := strings.TrimSpace(req.Note)
		if note == "" {
			note = strings.TrimSpace(req.Reason)
		}
		if !approve && note == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写驳回原因")
			return
		}
		x, err := s.loadAdjust(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "调账单不存在")
			return
		}
		if err != nil {
			dbErr(w, "adjust load", err)
			return
		}
		if x.CreatedBy == a.ID {
			s.audit(r, a, "ledger", "adjust_review", "user:"+x.UID, levelHigh, "refused:SELF_REVIEW", nil, map[string]any{"id": id}, note)
			common.WriteError(w, http.StatusForbidden, "SELF_REVIEW", "不能复核自己提交的调账单")
			return
		}
		if x.Status != "pending_approval" {
			common.WriteError(w, http.StatusConflict, "INVALID_STATE", "调账单状态为 "+x.Status+"，不可复核")
			return
		}
		if !s.require2FA(w, a, req.TOTPCode) {
			return
		}
		next := "rejected"
		if approve {
			next = "applying"
		}
		res, err := s.db.ExecContext(r.Context(), `UPDATE balance_adjustments SET status=$2, reviewed_by=$3, review_note=$4, reviewed_at=now(), reviewed_ip=$5
			WHERE id=$1 AND status='pending_approval' AND created_by <> $3`, id, next, a.ID, nullStr(note), s.clientIP(r))
		if err != nil {
			dbErr(w, "adjust review", err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			common.WriteError(w, http.StatusConflict, "INVALID_STATE", "调账单已被处理")
			return
		}
		if !approve {
			s.audit(r, a, "ledger", "adjust_reject", "user:"+x.UID, levelHigh, "ok", map[string]any{"status": "pending_approval"},
				map[string]any{"id": id, "status": "rejected", "amount": x.Amount, "asset": x.Asset}, note)
			x, _ = s.loadAdjust(r.Context(), id)
			common.WriteJSON(w, http.StatusOK, map[string]any{"adjustment": x})
			return
		}
		out, code := s.applyAdjust(r, a, id, "adjust_approve_apply", note)
		common.WriteJSON(w, code, map[string]any{"adjustment": out})
	}
}

func (s *Server) handleAdjustConfigGet(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, map[string]string{"secondReviewUsdt": s.adjustThreshold(r.Context()).String(),
		"defaultSecondReviewUsdt": s.cfg.AdjustSecondAmount.String()})
}

func (s *Server) handleAdjustConfigPut(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		SecondReviewUSDT string `json:"secondReviewUsdt"`
		Reason           string `json:"reason"`
		TOTPCode         string `json:"totpCode"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	v, err := decimal.NewFromString(strings.TrimSpace(req.SecondReviewUSDT))
	if err != nil || !v.IsPositive() {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "secondReviewUsdt 须为正数")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写操作原因")
		return
	}
	if !s.require2FA(w, a, req.TOTPCode) {
		return
	}
	before := s.adjustThreshold(r.Context()).String()
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO admin_settings (key, value, updated_by) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=now()`, adjustThresholdKey, v.String(), a.Username); err != nil {
		dbErr(w, "adjust config", err)
		return
	}
	s.audit(r, a, "ledger", "adjust_config", "setting:"+adjustThresholdKey, levelHigh, "ok",
		map[string]string{"secondReviewUsdt": before}, map[string]string{"secondReviewUsdt": v.String()}, strings.TrimSpace(req.Reason))
	s.handleAdjustConfigGet(w, r)
}
