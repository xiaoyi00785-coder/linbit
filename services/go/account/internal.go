package main

import (
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// internal.go holds the account service's service-to-service endpoints used by
// the wallet service to gate withdrawals. They require the shared internal token
// (X-Internal-Token); the gateway never proxies /internal/*.

// internalRequestToken authenticates a service-to-service call.
func (s *Server) authedInternal(r *http.Request) bool {
	return s.cfg.InternalToken != "" && r.Header.Get("X-Internal-Token") == s.cfg.InternalToken
}

type internalVerifyRequest struct {
	UID      string `json:"uid"`
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
	// Optional (security settings): "withdraw" | "internal_transfer". When set,
	// the withdrawal lock and fund-password policy are enforced too.
	Action       string `json:"action,omitempty"`
	FundPassword string `json:"fundPassword,omitempty"`
}

// Reasons: user_not_found, account_frozen, totp_required, totp_invalid,
// no_contact, code_required, code_invalid, withdraw_locked,
// fund_password_required, fund_password_invalid, fund_password_locked,
// fund_password_not_set, internal_error.
type internalVerifyResponse struct {
	OK          bool    `json:"ok"`
	Reason      string  `json:"reason,omitempty"`
	LockedUntil *string `json:"lockedUntil,omitempty"`
}

// handleInternalWithdrawVerify validates a withdrawal's second factor. When the
// user has TOTP enabled, totpCode must be valid; otherwise an email/SMS code with
// purpose=withdraw must be valid. It always answers 200 with {ok, reason} so the
// wallet can map the outcome to its own error codes.
func (s *Server) handleInternalWithdrawVerify(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var req internalVerifyRequest
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.UID) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	u, err := s.store.UserByUID(r.Context(), req.UID)
	if err != nil {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "user_not_found"})
		return
	}
	if u.Status == userFrozen {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "account_frozen"})
		return
	}
	// Second factor first (so a wrong fund password can't be probed without it).
	if u.TOTPEnabled {
		if req.TOTPCode == "" {
			common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "totp_required"})
			return
		}
		if !validateTOTP(u.TOTPSecret, req.TOTPCode) {
			common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "totp_invalid"})
			return
		}
		s.finishOutflowVerify(w, r, u, req)
		return
	}
	target := s.contactTarget(u, s.contactChannel(u))
	if target == "" {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "no_contact"})
		return
	}
	if req.Code == "" {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "code_required"})
		return
	}
	verdict, err := s.codes.Verify(r.Context(), target, purposeWithdraw, req.Code)
	if err != nil {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "internal_error"})
		return
	}
	if verdict != codeOK {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: "code_invalid"})
		return
	}
	s.finishOutflowVerify(w, r, u, req)
}

// finishOutflowVerify applies the withdrawal lock + fund password checks when
// the caller names an action (new wallet); legacy callers get 2FA-only.
func (s *Server) finishOutflowVerify(w http.ResponseWriter, r *http.Request, u *User, req internalVerifyRequest) {
	reason, until := s.outflowCheck(r.Context(), u, req.Action, req.FundPassword)
	if reason != "" {
		common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: false, Reason: reason, LockedUntil: until})
		return
	}
	common.WriteJSON(w, http.StatusOK, internalVerifyResponse{OK: true})
}

type internalWhitelistResponse struct {
	Enabled bool `json:"enabled"`
	Allowed bool `json:"allowed"`
}

// handleInternalWithdrawWhitelist reports whether the user's withdrawal whitelist
// is enabled and, if so, whether the destination (asset, network, address) is on
// it. When the whitelist is disabled, allowed is always true.
func (s *Server) handleInternalWithdrawWhitelist(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	q := r.URL.Query()
	uid := strings.TrimSpace(q.Get("uid"))
	if uid == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 uid")
		return
	}
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	if !u.WithdrawWhitelistEnabled {
		common.WriteJSON(w, http.StatusOK, internalWhitelistResponse{Enabled: false, Allowed: true})
		return
	}
	allowed, err := s.store.IsWhitelistedAddress(r.Context(), u.ID,
		q.Get("asset"), q.Get("network"), q.Get("address"))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, internalWhitelistResponse{Enabled: true, Allowed: allowed})
}

type internalKYCLevelResponse struct {
	UID   string `json:"uid"`
	Level int    `json:"level"`
}

// handleInternalGetKYCLevel reports a user's current kycLevel. Used by the wallet
// service to apply KYC-tiered withdrawal limits.
//
//	GET /internal/account/kyc-level?uid=
func (s *Server) handleInternalGetKYCLevel(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	if uid == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 uid")
		return
	}
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, internalKYCLevelResponse{UID: u.UID, Level: u.KYCLevel})
}

type internalSetKYCLevelRequest struct {
	UID   string `json:"uid"`
	Level int    `json:"level"`
}

// handleInternalSetKYCLevel raises a user's kycLevel (never downgrades). Called by
// the KYC service when an application is approved.
//
//	POST /internal/account/kyc-level   {uid, level}
func (s *Server) handleInternalSetKYCLevel(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var req internalSetKYCLevelRequest
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.UID) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.Level < 0 || req.Level > 9 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "认证等级不合法")
		return
	}
	u, err := s.store.UserByUID(r.Context(), req.UID)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	if err := s.store.SetKYCLevelAtLeast(r.Context(), u.UID, req.Level); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	u, _ = s.store.UserByUID(r.Context(), u.UID)
	common.WriteJSON(w, http.StatusOK, internalKYCLevelResponse{UID: u.UID, Level: u.KYCLevel})
}
