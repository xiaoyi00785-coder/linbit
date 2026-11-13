package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

type codeRequest struct {
	Code string `json:"code"`
}

type twoFactorStatus struct {
	Enabled bool    `json:"enabled"`
	Method  *string `json:"method"`
}

// totpVerifyRequest enables TOTP: Code is the authenticator's 6-digit code and
// SecurityCode is the code sent to the account's own email/phone
// (POST /account/code/send purpose=security).
type totpVerifyRequest struct {
	Code         string `json:"code"`
	SecurityCode string `json:"securityCode"`
}

// writeTOTPAlreadyEnabled rejects bind/verify while TOTP is on. Replacing the
// authenticator must go through /2fa/totp/disable (current TOTP + withdrawal
// lock) first, so a stolen session can never swap the secret silently.
// writeTOTPVerdict2FA maps a verifyTOTP verdict for the bind/verify/disable
// endpoints: a wrong code stays 400 TOTP_INVALID (the code those endpoints have
// always returned), while the lock and replay cases get their own codes.
func writeTOTPVerdict2FA(w http.ResponseWriter, v totpVerdict) bool {
	switch v {
	case totpOK:
		return true
	case totpLocked:
		common.WriteError(w, http.StatusTooManyRequests, "TOTP_LOCKED", "验证码错误次数过多，两步验证已暂时锁定，请 15 分钟后再试")
	case totpReplayed:
		common.WriteError(w, http.StatusBadRequest, "TOTP_REPLAYED", "该验证码已使用，请等待验证器显示新的验证码")
	default:
		common.WriteError(w, http.StatusBadRequest, "TOTP_INVALID", "验证码错误")
	}
	return false
}

func writeTOTPAlreadyEnabled(w http.ResponseWriter) {
	common.WriteError(w, http.StatusConflict, "TOTP_ALREADY_ENABLED", "已开启两步验证；如需更换身份验证器，请先关闭后再重新绑定")
}

func (s *Server) handleTOTPBind(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if u.TOTPEnabled {
		writeTOTPAlreadyEnabled(w)
		return
	}
	label := u.UID
	if u.Email.Valid {
		label = u.Email.String
	} else if u.Phone.Valid {
		label = u.Phone.String
	}
	secret, uri, err := generateTOTP(label)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.store.SetTOTPSecret(r.Context(), u.UID, secret); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauthUri": uri})
}

func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req totpVerifyRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if u.TOTPEnabled {
		writeTOTPAlreadyEnabled(w)
		return
	}
	if u.TOTPSecret == "" {
		common.WriteError(w, http.StatusBadRequest, "TOTP_NOT_BOUND", "请先绑定 TOTP")
		return
	}
	if !writeTOTPVerdict2FA(w, s.verifyTOTP(r.Context(), u, req.Code)) {
		return
	}
	// Enabling TOTP replaces email/SMS codes as the second factor for withdrawals
	// and security changes, so the account's current contact must confirm it: an
	// access token alone must not be able to hand the account's 2FA to someone
	// else. Checked after the TOTP code so a typo there doesn't burn the code.
	if !s.requireSensitiveAuth(w, r, u, "", req.SecurityCode, purposeSecurity) {
		return
	}
	if err := s.store.SetTOTPEnabled(r.Context(), u.UID, true); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.securityChanged(r.Context(), u, "开启了两步验证", false)
	method := "totp"
	common.WriteJSON(w, http.StatusOK, twoFactorStatus{Enabled: true, Method: &method})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req codeRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if !u.TOTPEnabled {
		common.WriteError(w, http.StatusBadRequest, "TOTP_NOT_ENABLED", "两步验证未启用")
		return
	}
	if !writeTOTPVerdict2FA(w, s.verifyTOTP(r.Context(), u, req.Code)) {
		return
	}
	if err := s.store.SetTOTPEnabled(r.Context(), u.UID, false); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.securityChanged(r.Context(), u, "关闭了两步验证", true)
	common.WriteJSON(w, http.StatusOK, twoFactorStatus{Enabled: false, Method: nil})
}
