package main

// 绑定、确认启用、关闭 TOTP。关闭走 securityChanged 且 lock=true，按 withdraw_lock_hours 加提现锁。
// 校验是 totp.Validate，不另存已用验证码。

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

func (s *Server) handleTOTPBind(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
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
	var req codeRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if u.TOTPSecret == "" {
		common.WriteError(w, http.StatusBadRequest, "TOTP_NOT_BOUND", "请先绑定 TOTP")
		return
	}
	if !validateTOTP(u.TOTPSecret, req.Code) {
		common.WriteError(w, http.StatusBadRequest, "TOTP_INVALID", "验证码错误")
		return
	}
	if err := s.store.SetTOTPEnabled(r.Context(), u.UID, true); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
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
	if !validateTOTP(u.TOTPSecret, req.Code) {
		common.WriteError(w, http.StatusBadRequest, "TOTP_INVALID", "验证码错误")
		return
	}
	if err := s.store.SetTOTPEnabled(r.Context(), u.UID, false); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.securityChanged(r.Context(), u, "关闭了两步验证", true)
	common.WriteJSON(w, http.StatusOK, twoFactorStatus{Enabled: false, Method: nil})
}
