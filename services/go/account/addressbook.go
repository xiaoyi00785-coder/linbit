package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

type patchAddressRequest struct {
	Label       *string `json:"label"`
	Memo        *string `json:"memo"`
	Whitelisted *bool   `json:"whitelisted"`
	TOTPCode    string  `json:"totpCode"`
	Code        string  `json:"code"`
}

// handlePatchAddress: PATCH /account/withdraw-addresses/{id} — edit label,
// memo and the whitelist flag (sensitive auth required).
func (s *Server) handlePatchAddress(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req patchAddressRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.Label != nil && len([]rune(strings.TrimSpace(*req.Label))) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "备注过长")
		return
	}
	if req.Memo != nil && len(strings.TrimSpace(*req.Memo)) > 128 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Memo 过长")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeAddress) {
		return
	}
	a, err := s.store.UpdateWithdrawAddress(r.Context(), u.ID, r.PathValue("id"), req.Label, req.Memo, req.Whitelisted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "地址不存在")
	case errors.Is(err, ErrAddressExists):
		common.WriteError(w, http.StatusConflict, "ADDRESS_EXISTS", "地址已存在")
	case err != nil:
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, a)
	}
}

// UpdateWithdrawAddress patches label / memo / whitelisted (nil = unchanged).
func (s *Store) UpdateWithdrawAddress(ctx context.Context, userID int64, id string, label, memo *string, wl *bool) (WithdrawAddress, error) {
	var a WithdrawAddress
	var l, m, b any
	if label != nil {
		l = strings.TrimSpace(*label)
	}
	if memo != nil {
		m = strings.TrimSpace(*memo)
	}
	if wl != nil {
		b = *wl
	}
	err := s.db.QueryRowContext(ctx,
		`UPDATE withdraw_addresses SET
		   label = COALESCE($3::text, label),
		   memo = COALESCE($4::text, memo),
		   whitelisted = COALESCE($5::boolean, whitelisted)
		 WHERE user_id = $1 AND id::text = $2
		 RETURNING id::text, asset, network, address, memo, label, whitelisted, created_at`,
		userID, id, l, m, b).Scan(&a.ID, &a.Asset, &a.Network, &a.Address, &a.Memo, &a.Label, &a.Whitelisted, &a.CreatedAt)
	if err != nil && uniqueConstraint(err) != "" {
		return a, ErrAddressExists
	}
	return a, err
}

type userLookupResponse struct {
	UID          string `json:"uid"`
	Status       string `json:"status"`
	IsSubAccount bool   `json:"isSubAccount"`
	MaskedEmail  string `json:"maskedEmail"`
}

func maskEmail(e string) string {
	if strings.IndexByte(e, '@') <= 0 {
		return ""
	}
	return common.MaskEmail(e)
}

// handleInternalUserLookup: GET /internal/users/lookup?uid=|email= — used by
// the wallet internal-transfer flow to resolve a recipient.
func (s *Server) handleInternalUserLookup(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	var (
		u   *User
		err error
	)
	switch {
	case uid != "":
		u, err = s.store.UserByUID(r.Context(), uid)
	case email != "":
		u, err = s.store.UserByEmail(r.Context(), email)
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 uid 或 email")
		return
	}
	if err != nil || u == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	sub, err := s.store.isSubAccount(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, userLookupResponse{UID: u.UID, Status: u.Status, IsSubAccount: sub, MaskedEmail: maskEmail(u.Email.String)})
}

func (s *Store) isSubAccount(ctx context.Context, id int64) (bool, error) {
	// Sub-accounts are users rows with parent_user_id set (account migration 00009).
	var sub bool
	err := s.db.QueryRowContext(ctx, `SELECT parent_user_id IS NOT NULL FROM users WHERE id = $1`, id).Scan(&sub)
	return sub, err
}
