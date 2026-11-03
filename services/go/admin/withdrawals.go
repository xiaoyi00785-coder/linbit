package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

type walletWithdrawal struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	Asset          string `json:"asset"`
	Network        string `json:"network"`
	Address        string `json:"address"`
	Amount         string `json:"amount"`
	GasFee         string `json:"gasFee"`
	FrozenAmount   string `json:"frozenAmount"`
	RiskScore      int    `json:"riskScore"`
	InternalStatus string `json:"internalStatus"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
}

type reviewState struct {
	NeedsSecond bool   `json:"needsSecondReview"`
	Stage       string `json:"stage"` // first | second
	FirstBy     *int64 `json:"firstReviewerId,omitempty"`
	FirstAt     string `json:"firstReviewedAt,omitempty"`
	Comment     string `json:"firstComment,omitempty"`
	USDTValue   string `json:"usdtValue,omitempty"`
}

type withdrawalView struct {
	walletWithdrawal
	Review reviewState `json:"review"`
}

func (s *Server) walletList(ctx context.Context, status string) ([]walletWithdrawal, upstream, error) {
	u, err := s.call(ctx, http.MethodGet, s.cfg.WalletURL, "/internal/admin/withdrawals?status="+url.QueryEscape(status), nil)
	if err != nil || !u.ok() {
		return nil, u, err
	}
	var body struct {
		Items []walletWithdrawal `json:"items"`
	}
	if err := u.decode(&body); err != nil {
		return nil, u, err
	}
	return body.Items, u, nil
}

// needsSecond decides whether a withdrawal needs a second reviewer: amount at
// or above the USDT threshold, risk above the risk threshold, or no price
// (fail closed).
func (s *Server) needsSecond(ctx context.Context, wd walletWithdrawal) (bool, string) {
	if wd.RiskScore > s.cfg.WithdrawSecondRisk {
		return true, ""
	}
	amt, err := decimal.NewFromString(wd.Amount)
	if err != nil {
		return true, ""
	}
	v, ok := s.usdtValue(ctx, wd.Asset, amt)
	if !ok {
		return true, ""
	}
	return v.GreaterThanOrEqual(s.cfg.WithdrawSecondAmount), v.StringFixed(2)
}

func (s *Server) loadReview(ctx context.Context, id string) (needs bool, firstBy sql.NullInt64, firstAt sql.NullTime, comment sql.NullString, status string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT needs_second, first_by, first_at, first_comment, status FROM withdrawal_reviews WHERE withdrawal_id = $1`, id).
		Scan(&needs, &firstBy, &firstAt, &comment, &status)
	return
}

func (s *Server) view(ctx context.Context, wd walletWithdrawal) withdrawalView {
	v := withdrawalView{walletWithdrawal: wd, Review: reviewState{Stage: "first"}}
	need, usdt := s.needsSecond(ctx, wd)
	v.Review.NeedsSecond, v.Review.USDTValue = need, usdt
	if _, by, at, c, st, err := s.loadReview(ctx, wd.ID); err == nil && st == "awaiting_second" {
		v.Review.Stage, v.Review.Comment = "second", c.String
		id := by.Int64
		v.Review.FirstBy, v.Review.FirstAt = &id, fmtTime(at.Time)
	}
	return v
}

func (s *Server) handleWithdrawals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending_review"
	}
	items, u, err := s.walletList(r.Context(), status)
	if err != nil {
		upstreamErr(w, "withdrawals", err)
		return
	}
	if !u.ok() {
		relay(w, u)
		return
	}
	out := make([]withdrawalView, 0, len(items))
	for _, wd := range items {
		out = append(out, s.view(r.Context(), wd))
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

func (s *Server) findPending(ctx context.Context, id string) (*walletWithdrawal, error) {
	items, u, err := s.walletList(ctx, "pending_review")
	if err != nil {
		return nil, err
	}
	if !u.ok() {
		return nil, errors.New("wallet status " + u.errCode())
	}
	for i := range items {
		if items[i].ID == id {
			return &items[i], nil
		}
	}
	return nil, nil
}

// handleWithdrawApprove runs the two-stage review. Stage 1 needs
// withdraw:first; when a second review is required the request is parked and
// stage 2 needs withdraw:second by a different admin. Every approval needs a
// comment and a fresh 2FA code.
func (s *Server) handleWithdrawApprove(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id := r.PathValue("id")
	var req reasonReq
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Comment) == "" {
		common.WriteError(w, http.StatusBadRequest, "COMMENT_REQUIRED", "审核意见必填")
		return
	}
	wd, err := s.findPending(r.Context(), id)
	if err != nil {
		upstreamErr(w, "withdraw approve", err)
		return
	}
	if wd == nil {
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该提现不在待审核状态")
		return
	}
	_, firstBy, _, _, status, lerr := s.loadReview(r.Context(), id)
	if lerr != nil && !errors.Is(lerr, sql.ErrNoRows) {
		internalErr(w, "load review", lerr)
		return
	}
	second := lerr == nil && status == "awaiting_second"
	need := false
	if second {
		if !can(a.Roles, permWithdrawSecond) {
			common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "需要复审权限")
			return
		}
		if firstBy.Int64 == a.ID {
			common.WriteError(w, http.StatusForbidden, "SAME_REVIEWER", "复审人不能与初审人相同")
			return
		}
	} else {
		if !can(a.Roles, permWithdrawFirst) {
			common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "需要初审权限")
			return
		}
		need, _ = s.needsSecond(r.Context(), *wd)
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	now := s.now()
	if !second && need {
		_, err := s.db.ExecContext(r.Context(), `
			INSERT INTO withdrawal_reviews (withdrawal_id, needs_second, first_by, first_at, first_comment, status, updated_at)
			VALUES ($1, true, $2, $3, $4, 'awaiting_second', $3)
			ON CONFLICT (withdrawal_id) DO UPDATE SET needs_second = true, first_by = $2, first_at = $3, first_comment = $4, status = 'awaiting_second', updated_at = $3`,
			id, a.ID, now, req.Comment)
		if err != nil {
			internalErr(w, "first review", err)
			return
		}
		s.audit(r, a, "withdraw", "first_approve", "withdrawal:"+id, levelHigh, "ok", wd, map[string]string{"stage": "awaiting_second"}, req.Comment)
		common.WriteJSON(w, http.StatusOK, map[string]interface{}{"id": id, "status": "awaiting_second_review"})
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.WalletURL, "/internal/admin/withdrawals/"+url.PathEscape(id)+"/approve", map[string]string{})
	if err != nil {
		upstreamErr(w, "wallet approve", err)
		return
	}
	action := "approve"
	if second {
		action = "second_approve"
	}
	if !u.ok() {
		s.audit(r, a, "withdraw", action, "withdrawal:"+id, levelHigh, "failed:"+u.errCode(), wd, nil, req.Comment)
		relay(w, u)
		return
	}
	if second {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE withdrawal_reviews SET second_by = $2, second_at = $3, second_comment = $4, status = 'approved', updated_at = $3 WHERE withdrawal_id = $1`, id, a.ID, now, req.Comment)
	} else {
		_, _ = s.db.ExecContext(r.Context(), `INSERT INTO withdrawal_reviews (withdrawal_id, needs_second, first_by, first_at, first_comment, status, updated_at)
			VALUES ($1, false, $2, $3, $4, 'approved', $3) ON CONFLICT (withdrawal_id) DO NOTHING`, id, a.ID, now, req.Comment)
	}
	s.audit(r, a, "withdraw", action, "withdrawal:"+id, levelHigh, "ok", wd, map[string]string{"status": "approved"}, req.Comment)
	relay(w, u)
}

// handleWithdrawReject rejects at either stage (reason + 2FA required).
func (s *Server) handleWithdrawReject(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id := r.PathValue("id")
	var req reasonReq
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
		return
	}
	if !can(a.Roles, permWithdrawFirst) && !can(a.Roles, permWithdrawSecond) {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "需要提现审核权限")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.WalletURL, "/internal/admin/withdrawals/"+url.PathEscape(id)+"/reject", map[string]string{"reason": req.Reason})
	if err != nil {
		upstreamErr(w, "wallet reject", err)
		return
	}
	if u.ok() {
		now := time.Now()
		_, _ = s.db.ExecContext(r.Context(), `INSERT INTO withdrawal_reviews (withdrawal_id, needs_second, status, updated_at) VALUES ($1, false, 'rejected', $2)
			ON CONFLICT (withdrawal_id) DO UPDATE SET status = 'rejected', updated_at = $2`, id, now)
		s.audit(r, a, "withdraw", "reject", "withdrawal:"+id, levelHigh, "ok", nil, map[string]string{"status": "rejected"}, req.Reason)
	} else {
		s.audit(r, a, "withdraw", "reject", "withdrawal:"+id, levelHigh, "failed:"+u.errCode(), nil, nil, req.Reason)
	}
	relay(w, u)
}
