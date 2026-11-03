package main

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
)

type updateProfileRequest struct {
	Nickname  *string `json:"nickname"`
	AvatarURL *string `json:"avatarUrl"`
}

// handleUpdateProfile updates the nickname and/or avatar URL.
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req updateProfileRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.Nickname == nil && req.AvatarURL == nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "未提供要修改的字段")
		return
	}
	if req.Nickname != nil {
		nick := strings.TrimSpace(*req.Nickname)
		if n := utf8.RuneCountInString(nick); n < 1 || n > 32 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "昵称长度需为 1–32 个字符")
			return
		}
		req.Nickname = &nick
	}
	if req.AvatarURL != nil {
		avatar := strings.TrimSpace(*req.AvatarURL)
		if avatar != "" && !validAvatarURL(avatar) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "头像 URL 不合法（需 http(s)，长度 ≤512）")
			return
		}
		req.AvatarURL = &avatar
	}
	if err := s.store.UpdateProfile(r.Context(), u.ID, req.Nickname, req.AvatarURL); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	u, _ = s.store.UserByUID(r.Context(), u.UID)
	common.WriteJSON(w, http.StatusOK, toAccountResponse(u))
}

// validAvatarURL checks the avatar URL is an http(s) URL within length limits.
func validAvatarURL(s string) bool {
	if len(s) > 512 {
		return false
	}
	parsed, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

type sendCodeAuthedRequest struct {
	Purpose string `json:"purpose"`
	Channel string `json:"channel"`
}

// handleSendCodeAuthed sends a verification code to the authenticated user's own
// contact (used before sensitive operations like editing the address book).
func (s *Server) handleSendCodeAuthed(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req sendCodeAuthedRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	switch req.Purpose {
	case purposeAddress, purposeBind, purposeReset, purposeWithdraw:
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不支持的 purpose")
		return
	}
	channel := req.Channel
	if channel == "" {
		channel = s.contactChannel(u)
	}
	target := s.contactTarget(u, channel)
	if target == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "账户未绑定对应的邮箱/手机")
		return
	}
	if err := s.codes.Send(r.Context(), channel, target, req.Purpose); err != nil {
		writeSendCodeError(w, "account code", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "channel": channel, "ttlSeconds": int(s.codes.ttl.Seconds())})
}

// requireSensitiveAuth gates a sensitive change behind TOTP (when enabled) or an
// emailed/texted code with the given purpose. It writes an API error and returns
// false when the check fails.
func (s *Server) requireSensitiveAuth(w http.ResponseWriter, r *http.Request, u *User, totpCode, code, purpose string) bool {
	if u.TOTPEnabled {
		if totpCode == "" || !validateTOTP(u.TOTPSecret, totpCode) {
			common.WriteError(w, http.StatusUnauthorized, "TOTP_INVALID", "需要有效的两步验证码")
			return false
		}
		return true
	}
	if code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请先获取并提交验证码")
		return false
	}
	target := s.contactTarget(u, s.contactChannel(u))
	verdict, err := s.codes.Verify(r.Context(), target, purpose, code)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return false
	}
	return !writeCodeVerdict(w, verdict)
}
