package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// loginChallengeTTL bounds how long a two-step login challenge stays valid.
const loginChallengeTTL = 5 * time.Minute

// identity is a resolved registration/login handle: a channel (email|sms) plus
// the normalized target (lowercased email or E.164 phone).
type identity struct {
	channel     string
	target      string
	countryCode string
}

// Identity-resolution errors (in addition to the phone errors in phone.go).
var (
	// errNoIdentity means neither email nor phone was supplied.
	errNoIdentity = errors.New("请提供邮箱或手机号")
	// errBadEmail means the email failed the sanity check.
	errBadEmail = errors.New("邮箱格式不正确")
)

// resolveIdentity turns an email or phone (+ dial country code) into an identity.
// Email takes precedence when both are supplied. On failure it returns a
// specific error so callers can return 400 with a clear, consistent message.
func resolveIdentity(email, phone, countryCode string) (identity, error) {
	email = strings.TrimSpace(email)
	phone = strings.TrimSpace(phone)
	switch {
	case email != "":
		if !validEmail(email) {
			return identity{}, errBadEmail
		}
		return identity{channel: channelEmail, target: strings.ToLower(email)}, nil
	case phone != "":
		norm, dial, err := normalizePhone(phone, countryCode)
		if err != nil {
			return identity{}, err
		}
		return identity{channel: channelSMS, target: norm, countryCode: dial}, nil
	default:
		return identity{}, errNoIdentity
	}
}

// writeIdentityError maps a resolveIdentity error to a 400 with a precise,
// user-facing message. It is used by every entry point (register / send-code /
// login / verify-code) so a malformed phone or missing country code is always a
// clear 400 — never a misleading 401.
func writeIdentityError(w http.ResponseWriter, err error) {
	msg := "邮箱或手机号不合法"
	switch {
	case errors.Is(err, errNoIdentity):
		msg = "请提供邮箱或手机号"
	case errors.Is(err, errBadEmail):
		msg = "邮箱格式不正确"
	case errors.Is(err, ErrPhoneCountryCode):
		msg = "请提供手机号国家码（如 86）"
	case errors.Is(err, ErrPhoneFormat):
		msg = "手机号格式不正确"
	}
	common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", msg)
}

// writeCodeVerdict maps a non-OK code verdict to an API error. It returns true
// when it wrote an error (i.e. the verdict was not codeOK).
func writeCodeVerdict(w http.ResponseWriter, v codeVerdict) bool {
	switch v {
	case codeOK:
		return false
	case codeTooManyAttempts:
		common.WriteError(w, http.StatusBadRequest, "CODE_TOO_MANY_ATTEMPTS", "验证码尝试次数过多，请重新获取")
	default:
		common.WriteError(w, http.StatusBadRequest, "CODE_INVALID", "验证码无效或已过期")
	}
	return true
}

// ---- request DTOs ----

type sendCodeRequest struct {
	Purpose     string `json:"purpose"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
}

type registerRequest struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
	Password    string `json:"password"`
	Code        string `json:"code"`
	InviteCode  string `json:"inviteCode"`
}

type loginRequest struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
	Password    string `json:"password"`
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
}

type loginVerifyRequest struct {
	ChallengeToken string `json:"challengeToken"`
	TOTPCode       string `json:"totpCode"`
	Code           string `json:"code"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// verifyResetCodeRequest is step 1 of the forgot-password flow: validate the
// emailed/texted code and mint a short-lived reset token.
type verifyResetCodeRequest struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
	Code        string `json:"code"`
}

// verifyResetCodeResponse carries the single-use reset token and its lifetime.
type verifyResetCodeResponse struct {
	ResetToken string `json:"resetToken"`
	ExpiresIn  int    `json:"expiresIn"`
}

// passwordResetRequest is step 2: exchange the reset token for a new password.
type passwordResetRequest struct {
	ResetToken  string `json:"resetToken"`
	NewPassword string `json:"newPassword"`
}

type loginChallengeResponse struct {
	ChallengeToken string   `json:"challengeToken"`
	Methods        []string `json:"methods"`
	ExpiresIn      int      `json:"expiresIn"`
}

// ---- handlers ----

// handleSendCode issues a verification code for a pre-auth flow (register or
// reset_password). To avoid leaking which accounts exist, reset_password always
// returns 200 and only actually sends when the account exists.
func (s *Server) handleSendCode(w http.ResponseWriter, r *http.Request) {
	var req sendCodeRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.Purpose != purposeRegister && req.Purpose != purposeReset {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "purpose 仅支持 register / reset_password")
		return
	}
	id, err := resolveIdentity(req.Email, req.Phone, req.CountryCode)
	if err != nil {
		writeIdentityError(w, err)
		return
	}

	if req.Purpose == purposeReset {
		// Only send when the account exists; respond 200 either way.
		if _, err := s.lookupByIdentity(r.Context(), id); err != nil {
			common.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "channel": id.channel, "ttlSeconds": int(s.codes.ttl.Seconds())})
			return
		}
	}

	if err := s.codes.Send(r.Context(), id.channel, id.target, req.Purpose); err != nil {
		if errors.Is(err, ErrRateLimited) {
			common.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "验证码发送过于频繁，请稍后再试")
			return
		}
		log.Printf("send code: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "channel": id.channel, "ttlSeconds": int(s.codes.ttl.Seconds())})
}

func (s *Server) lookupByIdentity(ctx context.Context, id identity) (*User, error) {
	if id.channel == channelEmail {
		return s.store.UserByEmail(ctx, id.target)
	}
	return s.store.UserByPhone(ctx, id.target)
}

// handleRegister creates an account after validating a registration code.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	id, err := resolveIdentity(req.Email, req.Phone, req.CountryCode)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if len(req.Password) < 8 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "密码至少 8 位")
		return
	}
	if req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请提供验证码")
		return
	}
	verdict, err := s.codes.Verify(r.Context(), id.target, purposeRegister, req.Code)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if writeCodeVerdict(w, verdict) {
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	params := CreateUserParams{PasswordHash: hash, CountryCode: id.countryCode}
	if id.channel == channelEmail {
		params.Email = &id.target
		params.EmailVerified = true
	} else {
		params.Phone = &id.target
		params.PhoneVerified = true
	}
	u, err := s.store.CreateUser(r.Context(), params)
	if errors.Is(err, ErrEmailExists) {
		common.WriteError(w, http.StatusConflict, "AUTH_EMAIL_EXISTS", "该邮箱已被注册")
		return
	}
	if errors.Is(err, ErrPhoneExists) {
		common.WriteError(w, http.StatusConflict, "AUTH_PHONE_EXISTS", "该手机号已被注册")
		return
	}
	if err != nil {
		log.Printf("register: create user: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.initAssetAccount(r.Context(), u.UID)

	// Trust the registering device so an immediate login from it is not treated
	// as a new device.
	dev := deviceFrom(r, "", "注册设备")
	if err := s.store.UpsertDevice(r.Context(), u.ID, dev, s.now()); err != nil {
		log.Printf("register: upsert device: %v", err)
	}
	tokens, err := s.issueTokens(r.Context(), u, dev.DeviceKey)
	if err != nil {
		if errors.Is(err, ErrAccountFrozen) {
			writeFrozen(w)
			return
		}
		log.Printf("register: issue tokens: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, tokens)
}

// handleLogin verifies the password, then either issues tokens directly (known
// device, no 2FA) or returns a challenge requiring a second factor.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	id, err := resolveIdentity(req.Email, req.Phone, req.CountryCode)
	if err != nil {
		// A malformed handle (bad email, bad phone format, or a bare number with
		// no country code) is a client input error: return 400 with a clear
		// message, consistent with register/send-code — not a misleading 401.
		writeIdentityError(w, err)
		return
	}
	u, err := s.lookupByIdentity(r.Context(), id)
	if err != nil || !checkPassword(u.PasswordHash, req.Password) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "账号或密码错误")
		return
	}
	if u.Status == userFrozen {
		writeFrozen(w)
		return
	}

	dev := deviceFrom(r, req.DeviceID, req.DeviceName)

	// Decide the second factor: TOTP wins; otherwise a new device triggers an
	// email/SMS code. A known device with no TOTP logs in directly.
	method := ""
	if u.TOTPEnabled {
		method = "totp"
	} else {
		known, err := s.store.DeviceExists(r.Context(), u.ID, dev.DeviceKey)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if !known {
			method = s.contactChannel(u)
		}
	}

	if method == "" {
		_ = s.store.UpsertDevice(r.Context(), u.ID, dev, s.now())
		tokens, err := s.issueTokens(r.Context(), u, dev.DeviceKey)
		if err != nil {
			if errors.Is(err, ErrAccountFrozen) {
				writeFrozen(w)
				return
			}
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		common.WriteJSON(w, http.StatusOK, tokens)
		return
	}

	challenge, err := newRefreshToken()
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.store.StoreLoginChallenge(r.Context(), u.ID, challenge, method, dev, s.now().Add(loginChallengeTTL)); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	// For email/SMS challenges, dispatch the code now (best-effort: a rate-limit
	// just means a recently-sent code is still valid).
	if method == channelEmail || method == channelSMS {
		if target := s.contactTarget(u, method); target != "" {
			if err := s.codes.Send(r.Context(), method, target, purposeLogin); err != nil && !errors.Is(err, ErrRateLimited) {
				log.Printf("login: send code: %v", err)
			}
		}
	}
	common.WriteJSON(w, http.StatusOK, loginChallengeResponse{
		ChallengeToken: challenge,
		Methods:        []string{method},
		ExpiresIn:      int(loginChallengeTTL.Seconds()),
	})
}

// handleLoginVerify completes a two-step login by validating the second factor.
func (s *Server) handleLoginVerify(w http.ResponseWriter, r *http.Request) {
	var req loginVerifyRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.ChallengeToken == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	c, err := s.store.LookupLoginChallenge(r.Context(), req.ChallengeToken)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "CHALLENGE_INVALID", "验证会话无效或已过期")
		return
	}
	u, err := s.store.UserByID(r.Context(), c.UserID)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "CHALLENGE_INVALID", "验证会话无效或已过期")
		return
	}

	switch c.Method {
	case "totp":
		if req.TOTPCode == "" || !validateTOTP(u.TOTPSecret, req.TOTPCode) {
			common.WriteError(w, http.StatusUnauthorized, "TOTP_INVALID", "两步验证码错误")
			return
		}
	case channelEmail, channelSMS:
		if req.Code == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请提供验证码")
			return
		}
		target := s.contactTarget(u, c.Method)
		verdict, err := s.codes.Verify(r.Context(), target, purposeLogin, req.Code)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if writeCodeVerdict(w, verdict) {
			return
		}
	default:
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "未知验证方式")
		return
	}

	_ = s.store.ConsumeLoginChallenge(r.Context(), c.ID)
	dev := Device{DeviceKey: c.DeviceKey, Name: c.DeviceName, UserAgent: c.UserAgent, IP: c.IP}
	_ = s.store.UpsertDevice(r.Context(), u.ID, dev, s.now())
	tokens, err := s.issueTokens(r.Context(), u, dev.DeviceKey)
	if err != nil {
		if errors.Is(err, ErrAccountFrozen) {
			writeFrozen(w)
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, tokens)
}

// handleLoginCode re-sends the email/SMS code for an in-flight challenge.
func (s *Server) handleLoginCode(w http.ResponseWriter, r *http.Request) {
	var req loginVerifyRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.ChallengeToken == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	c, err := s.store.LookupLoginChallenge(r.Context(), req.ChallengeToken)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "CHALLENGE_INVALID", "验证会话无效或已过期")
		return
	}
	if c.Method != channelEmail && c.Method != channelSMS {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "该验证方式无需发送验证码")
		return
	}
	u, err := s.store.UserByID(r.Context(), c.UserID)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "CHALLENGE_INVALID", "验证会话无效或已过期")
		return
	}
	if err := s.codes.Send(r.Context(), c.Method, s.contactTarget(u, c.Method), purposeLogin); err != nil {
		if errors.Is(err, ErrRateLimited) {
			common.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "验证码发送过于频繁，请稍后再试")
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "channel": c.Method})
}

// handleRefresh rotates a refresh token, keeping its device binding.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.RefreshToken == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	rec, err := s.store.ConsumeRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "刷新令牌无效或已过期")
		return
	}
	tokens, err := s.issueTokens(r.Context(), rec.User, rec.DeviceKey)
	if err != nil {
		if errors.Is(err, ErrAccountFrozen) {
			writeFrozen(w)
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, tokens)
}

// handleLogout revokes every active session for the user.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if err := s.store.RevokeAllRefreshTokens(r.Context(), u.ID); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePasswordVerifyCode is step 1 of the forgot-password flow: it validates
// the reset code and, on success, issues a short-lived single-use reset token.
// The subsequent /auth/password/reset call exchanges that token for a new
// password, so the raw code never travels with the password change.
func (s *Server) handlePasswordVerifyCode(w http.ResponseWriter, r *http.Request) {
	var req verifyResetCodeRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	id, err := resolveIdentity(req.Email, req.Phone, req.CountryCode)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请提供验证码")
		return
	}
	verdict, err := s.codes.Verify(r.Context(), id.target, purposeReset, req.Code)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if writeCodeVerdict(w, verdict) {
		return
	}
	// A valid reset code is only ever issued to an existing account, so a miss
	// here is treated as an invalid code (and avoids account enumeration).
	u, err := s.lookupByIdentity(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "CODE_INVALID", "验证码无效或已过期")
		return
	}
	token, err := newRefreshToken()
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.store.StorePasswordResetToken(r.Context(), u.ID, token, s.now().Add(s.cfg.ResetTokenTTL)); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, verifyResetCodeResponse{
		ResetToken: token,
		ExpiresIn:  int(s.cfg.ResetTokenTTL.Seconds()),
	})
}

// handlePasswordReset is step 2 of the forgot-password flow: it consumes the
// single-use reset token, sets the new password, and revokes all sessions.
func (s *Server) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.ResetToken == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 resetToken")
		return
	}
	if len(req.NewPassword) < 8 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "密码至少 8 位")
		return
	}
	userID, err := s.store.ConsumePasswordResetToken(r.Context(), req.ResetToken)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "RESET_TOKEN_INVALID", "重置令牌无效或已过期")
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.store.UpdatePassword(r.Context(), userID, hash); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.store.RevokeAllRefreshTokens(r.Context(), userID); err != nil {
		log.Printf("password reset: revoke tokens: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// contactChannel returns the preferred second-factor channel for a user
// (SMS when a phone is on file, else email).
func (s *Server) contactChannel(u *User) string {
	if u.Phone.Valid {
		return channelSMS
	}
	if u.Email.Valid {
		return channelEmail
	}
	return ""
}

// contactTarget returns the destination (phone or email) for a channel.
func (s *Server) contactTarget(u *User, channel string) string {
	switch channel {
	case channelSMS:
		if u.Phone.Valid {
			return u.Phone.String
		}
	case channelEmail:
		if u.Email.Valid {
			return u.Email.String
		}
	}
	return ""
}
