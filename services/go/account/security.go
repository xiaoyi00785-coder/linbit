package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// security.go: account security settings — password change, email/phone
// change, fund password, anti-phishing code, sessions, security overview, the
// post-change withdrawal lock and admin-tunable security options.

const purposeSecurity = "security" // code to the user's current contact
const purposeBindNew = "bind_new"  // code to a new email/phone being bound

// ---- admin-tunable settings (account_settings) ----

type securitySettings struct {
	WithdrawLockHours           int  `json:"withdrawLockHours"`       // 0 disables the post-change lock
	FundPasswordForWithdraw     bool `json:"fundPasswordForWithdraw"` // require fund password (when set) on withdraw
	FundPasswordForInternalXfer bool `json:"fundPasswordForInternalTransfer"`
	FundPasswordMandatory       bool `json:"fundPasswordMandatory"` // users without one are refused outflows
	FundPasswordMaxAttempts     int  `json:"fundPasswordMaxAttempts"`
	FundPasswordLockMinutes     int  `json:"fundPasswordLockMinutes"`
	LockAppliesToInternalXfer   bool `json:"lockAppliesToInternalTransfer"`
}

func defaultSecuritySettings() securitySettings {
	return securitySettings{WithdrawLockHours: 24, FundPasswordForWithdraw: true, FundPasswordForInternalXfer: true,
		FundPasswordMaxAttempts: 5, FundPasswordLockMinutes: 1440, LockAppliesToInternalXfer: true}
}

var settingKeys = map[string]func(*securitySettings, string){
	"withdraw_lock_hours":               func(c *securitySettings, v string) { c.WithdrawLockHours = atoiDef(v, c.WithdrawLockHours) },
	"fund_password_withdraw":            func(c *securitySettings, v string) { c.FundPasswordForWithdraw = v == "true" },
	"fund_password_internal_transfer":   func(c *securitySettings, v string) { c.FundPasswordForInternalXfer = v == "true" },
	"fund_password_mandatory":           func(c *securitySettings, v string) { c.FundPasswordMandatory = v == "true" },
	"fund_password_max_attempts":        func(c *securitySettings, v string) { c.FundPasswordMaxAttempts = atoiDef(v, c.FundPasswordMaxAttempts) },
	"fund_password_lock_minutes":        func(c *securitySettings, v string) { c.FundPasswordLockMinutes = atoiDef(v, c.FundPasswordLockMinutes) },
	"lock_applies_to_internal_transfer": func(c *securitySettings, v string) { c.LockAppliesToInternalXfer = v == "true" },
}

func atoiDef(v string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return n
	}
	return def
}

func (s *Store) SecuritySettings(ctx context.Context) (securitySettings, error) {
	c := defaultSecuritySettings()
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM account_settings`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return c, err
		}
		if f := settingKeys[k]; f != nil {
			f(&c, v)
		}
	}
	return c, rows.Err()
}

func (s *Store) putSetting(ctx context.Context, k, v, by string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_settings (key,value,updated_by,updated_at) VALUES ($1,$2,$3,now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=now()`, k, v, by)
	return err
}

// ---- per-user security columns ----

type secInfo struct {
	FundHash     string
	FundFailed   int
	FundLocked   sql.NullTime
	AntiPhishing string
	WithdrawLock sql.NullTime
	LockReason   string
}

func (s *Store) secInfo(ctx context.Context, userID int64) (secInfo, error) {
	var i secInfo
	err := s.db.QueryRowContext(ctx, `SELECT fund_password_hash, fund_pw_failed, fund_pw_locked_until,
		anti_phishing_code, withdraw_locked_until, withdraw_lock_reason FROM users WHERE id=$1`, userID).
		Scan(&i.FundHash, &i.FundFailed, &i.FundLocked, &i.AntiPhishing, &i.WithdrawLock, &i.LockReason)
	return i, err
}

func (s *Store) antiPhishingByEmail(ctx context.Context, email string) string {
	var c string
	_ = s.db.QueryRowContext(ctx, `SELECT anti_phishing_code FROM users WHERE email=$1`, strings.ToLower(email)).Scan(&c)
	return c
}

func (s *Store) setWithdrawLock(ctx context.Context, userID int64, until time.Time, reason string) error {
	// Never shorten an existing lock.
	_, err := s.db.ExecContext(ctx, `UPDATE users SET withdraw_locked_until = GREATEST(COALESCE(withdraw_locked_until, $2), $2),
		withdraw_lock_reason=$3 WHERE id=$1`, userID, until, reason)
	return err
}

func (s *Store) clearWithdrawLock(ctx context.Context, uid string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET withdraw_locked_until=NULL, withdraw_lock_reason='' WHERE uid=$1`, uid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) setFundPassword(ctx context.Context, userID int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET fund_password_hash=$2, fund_pw_failed=0, fund_pw_locked_until=NULL WHERE id=$1`, userID, hash)
	return err
}

// fundFail records a wrong fund password and locks after max attempts.
func (s *Store) fundFail(ctx context.Context, userID int64, max int, lock time.Duration, now time.Time) (locked bool, err error) {
	var n int
	if err = s.db.QueryRowContext(ctx, `UPDATE users SET fund_pw_failed=fund_pw_failed+1 WHERE id=$1 RETURNING fund_pw_failed`, userID).Scan(&n); err != nil {
		return false, err
	}
	if max > 0 && n >= max {
		_, err = s.db.ExecContext(ctx, `UPDATE users SET fund_pw_locked_until=$2, fund_pw_failed=0 WHERE id=$1`, userID, now.Add(lock))
		return true, err
	}
	return false, nil
}

func (s *Store) fundOK(ctx context.Context, userID int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET fund_pw_failed=0 WHERE id=$1`, userID)
}

func (s *Store) setAntiPhishing(ctx context.Context, userID int64, code string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET anti_phishing_code=$2 WHERE id=$1`, userID, code)
	return err
}

func (s *Store) updateContact(ctx context.Context, userID int64, email, phone *string, cc string) error {
	var err error
	if email != nil {
		_, err = s.db.ExecContext(ctx, `UPDATE users SET email=$2, email_verified=TRUE WHERE id=$1`, userID, strings.ToLower(*email))
	} else if phone != nil {
		_, err = s.db.ExecContext(ctx, `UPDATE users SET phone=$2, country_code=$3, phone_verified=TRUE WHERE id=$1`, userID, *phone, cc)
	}
	if c := uniqueConstraint(err); c != "" {
		if strings.Contains(c, "phone") {
			return ErrPhoneExists
		}
		return ErrEmailExists
	}
	return err
}

// ---- sessions (active refresh tokens) ----

type sessionDTO struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"deviceId"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	Current   bool      `json:"current"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Store) listSessions(ctx context.Context, userID int64) ([]sessionDTO, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.device_key, COALESCE(d.name,''), COALESCE(d.ip,''), COALESCE(d.user_agent,''),
		t.created_at, t.expires_at FROM refresh_tokens t
		LEFT JOIN devices d ON d.user_id=t.user_id AND d.device_key=t.device_key
		WHERE t.user_id=$1 AND t.revoked=FALSE AND t.expires_at>now() ORDER BY t.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sessionDTO{}
	for rows.Next() {
		var d sessionDTO
		var id int64
		if err := rows.Scan(&id, &d.DeviceID, &d.Name, &d.IP, &d.UserAgent, &d.CreatedAt, &d.ExpiresAt); err != nil {
			return nil, err
		}
		d.ID = strconv.FormatInt(id, 10)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) revokeSession(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked=TRUE WHERE user_id=$1 AND id::text=$2 AND revoked=FALSE`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) revokeOtherSessions(ctx context.Context, userID int64, keepDevice string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked=TRUE WHERE user_id=$1 AND device_key<>$2 AND revoked=FALSE`, userID, keepDevice)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- helpers ----

// securityChanged applies the post-change withdrawal lock and sends a security
// notification (in-app + forced email).
func (s *Server) securityChanged(ctx context.Context, u *User, what string, lock bool) {
	until := ""
	if lock {
		if cfg, err := s.store.SecuritySettings(ctx); err == nil && cfg.WithdrawLockHours > 0 {
			t := s.now().Add(time.Duration(cfg.WithdrawLockHours) * time.Hour)
			if err := s.store.setWithdrawLock(ctx, u.ID, t, what); err != nil {
				log.Printf("security: set withdraw lock: %v", err)
			}
			until = "为保障资金安全，" + strconv.Itoa(cfg.WithdrawLockHours) + " 小时内（至 " + t.UTC().Format("2006-01-02 15:04") + " UTC）暂停提现和站内转账。"
		}
	}
	body := "您的账户于 " + s.now().UTC().Format("2006-01-02 15:04:05") + " (UTC) " + what + "。" + until + "如非本人操作，请立即联系客服冻结账户。"
	force := true
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notify(c, u, common.NoticeSecurity, "账户安全设置变更："+what, body, &force); err != nil {
			log.Printf("security notify: %v", err)
		}
	}()
}

// requireTOTPIfEnabled checks TOTP only when the user has it enabled.
func requireTOTPIfEnabled(w http.ResponseWriter, u *User, totp string) bool {
	if !u.TOTPEnabled {
		return true
	}
	if totp == "" {
		common.WriteError(w, http.StatusForbidden, "TWOFA_REQUIRED", "请提供两步验证码")
		return false
	}
	if !validateTOTP(u.TOTPSecret, totp) {
		common.WriteError(w, http.StatusUnauthorized, "INVALID_TOTP", "两步验证码错误")
		return false
	}
	return true
}

// verifyCodeAt verifies a code sent to a specific target.
func (s *Server) verifyCodeAt(w http.ResponseWriter, r *http.Request, target, purpose, code string) bool {
	if code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请先获取并提交验证码")
		return false
	}
	v, err := s.codes.Verify(r.Context(), target, purpose, code)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return false
	}
	return !writeCodeVerdict(w, v)
}

func internalErr(w http.ResponseWriter) {
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func (s *Server) mustUser(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
	}
	return u, ok
}

func timePtr(t sql.NullTime, now time.Time) *string {
	if !t.Valid || !t.Time.After(now) {
		return nil
	}
	v := t.Time.UTC().Format(time.RFC3339)
	return &v
}

// ---- GET /account/security ----

// securityLevel scores the account's protection: one point each for a verified
// email, verified phone, 2FA (TOTP or passkey), fund password, anti-phishing code.
type securityLevel struct {
	Score int    `json:"score"`
	Max   int    `json:"max"`
	Level string `json:"level"` // low | medium | high
	// Missing lists the unset items (emailVerified, phoneVerified, twoFactor, fundPassword, antiPhishing).
	Missing []string `json:"missing"`
}

func computeSecurityLevel(emailV, phoneV, twoFA, fund, anti bool) securityLevel {
	l := securityLevel{Max: 5, Missing: []string{}}
	for _, it := range []struct {
		ok   bool
		name string
	}{{emailV, "emailVerified"}, {phoneV, "phoneVerified"}, {twoFA, "twoFactor"}, {fund, "fundPassword"}, {anti, "antiPhishing"}} {
		if it.ok {
			l.Score++
		} else {
			l.Missing = append(l.Missing, it.name)
		}
	}
	switch {
	case l.Score >= 4:
		l.Level = "high"
	case l.Score >= 2:
		l.Level = "medium"
	default:
		l.Level = "low"
	}
	return l
}

type securityOverview struct {
	SecurityLevel securityLevel `json:"securityLevel"`
	TwoFactor     struct {
		Enabled bool    `json:"enabled"`
		Method  *string `json:"method"`
	} `json:"twoFactor"`
	Email                   *string  `json:"email"`
	EmailVerified           bool     `json:"emailVerified"`
	Phone                   *string  `json:"phone"`
	PhoneVerified           bool     `json:"phoneVerified"`
	FundPasswordSet         bool     `json:"fundPasswordSet"`
	FundPasswordLockedUntil *string  `json:"fundPasswordLockedUntil"`
	AntiPhishingSet         bool     `json:"antiPhishingSet"`
	AntiPhishingHint        *string  `json:"antiPhishingHint"`
	PasskeysCount           int      `json:"passkeysCount"`
	WithdrawLockedUntil     *string  `json:"withdrawLockedUntil"`
	WithdrawLockReason      *string  `json:"withdrawLockReason"`
	RecentDevices           []Device `json:"recentDevices"`
	Settings                struct {
		FundPasswordForWithdraw     bool `json:"fundPasswordForWithdraw"`
		FundPasswordForInternalXfer bool `json:"fundPasswordForInternalTransfer"`
		FundPasswordMandatory       bool `json:"fundPasswordMandatory"`
		WithdrawLockHours           int  `json:"withdrawLockHours"`
	} `json:"policy"`
}

func maskEmailStr(e string) string { return maskEmail(e) }

func maskPhoneStr(p string) string { return common.MaskPhone(p) }

func (s *Server) handleSecurityOverview(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	si, err := s.store.secInfo(ctx, u.ID)
	if err != nil {
		internalErr(w)
		return
	}
	cfg, _ := s.store.SecuritySettings(ctx)
	var o securityOverview
	o.TwoFactor.Enabled = u.TOTPEnabled
	if u.TOTPEnabled {
		m := "totp"
		o.TwoFactor.Method = &m
	}
	if u.Email.Valid {
		m := maskEmailStr(u.Email.String)
		o.Email = &m
	}
	if u.Phone.Valid {
		m := maskPhoneStr(u.Phone.String)
		o.Phone = &m
	}
	o.EmailVerified, o.PhoneVerified = u.EmailVerified, u.PhoneVerified
	now := s.now()
	o.FundPasswordSet = si.FundHash != ""
	o.FundPasswordLockedUntil = timePtr(si.FundLocked, now)
	o.AntiPhishingSet = si.AntiPhishing != ""
	if o.AntiPhishingSet {
		h := si.AntiPhishing[:1] + strings.Repeat("*", len([]rune(si.AntiPhishing))-1)
		o.AntiPhishingHint = &h
	}
	o.PasskeysCount, _ = s.store.countPasskeys(ctx, u.ID)
	o.WithdrawLockedUntil = timePtr(si.WithdrawLock, now)
	o.SecurityLevel = computeSecurityLevel(u.EmailVerified, u.PhoneVerified, u.TOTPEnabled || o.PasskeysCount > 0, o.FundPasswordSet, o.AntiPhishingSet)
	if o.WithdrawLockedUntil != nil && si.LockReason != "" {
		o.WithdrawLockReason = &si.LockReason
	}
	devs, _ := s.store.ListDevices(ctx, u.ID)
	if len(devs) > 5 {
		devs = devs[:5]
	}
	cur := deviceKeyFor(r, "")
	for i := range devs {
		devs[i].Current = devs[i].DeviceKey == cur
	}
	if devs == nil {
		devs = []Device{}
	}
	o.RecentDevices = devs
	o.Settings.FundPasswordForWithdraw = cfg.FundPasswordForWithdraw
	o.Settings.FundPasswordForInternalXfer = cfg.FundPasswordForInternalXfer
	o.Settings.FundPasswordMandatory = cfg.FundPasswordMandatory
	o.Settings.WithdrawLockHours = cfg.WithdrawLockHours
	common.WriteJSON(w, http.StatusOK, o)
}

// ---- POST /account/password ----

type changePasswordReq struct {
	OldPassword         string `json:"oldPassword"`
	NewPassword         string `json:"newPassword"`
	TOTPCode            string `json:"totpCode"`
	Code                string `json:"code"`
	RevokeOtherSessions *bool  `json:"revokeOtherSessions"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	var req changePasswordReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if len(req.NewPassword) < 8 || len(req.NewPassword) > 128 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "密码长度需为 8-128 位")
		return
	}
	if !checkPassword(u.PasswordHash, req.OldPassword) {
		common.WriteError(w, http.StatusBadRequest, "OLD_PASSWORD_INVALID", "原密码错误")
		return
	}
	if req.NewPassword == req.OldPassword {
		common.WriteError(w, http.StatusBadRequest, "PASSWORD_UNCHANGED", "新密码不能与原密码相同")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil || s.store.UpdatePassword(r.Context(), u.ID, hash) != nil {
		internalErr(w)
		return
	}
	var revoked int64
	if req.RevokeOtherSessions == nil || *req.RevokeOtherSessions {
		revoked, _ = s.store.revokeOtherSessions(r.Context(), u.ID, deviceKeyFor(r, ""))
	}
	s.securityChanged(r.Context(), u, "修改了登录密码", true)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "revokedSessions": revoked})
}

// ---- email / phone change ----

type contactCodeReq struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
}

func (s *Server) smsAvailable() bool { return s.cfg.DevMode || s.cfg.SMSEnabled }

// POST /account/contact/code — send a bind code to the NEW email/phone.
func (s *Server) handleContactCode(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	var req contactCodeReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	id, err := resolveIdentity(req.Email, req.Phone, req.CountryCode)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if id.channel == channelSMS && !s.smsAvailable() {
		common.WriteError(w, http.StatusServiceUnavailable, "SMS_UNAVAILABLE", "短信服务暂不可用，请稍后再试")
		return
	}
	if ex, err := s.lookupByIdentity(r.Context(), id); err == nil && ex.ID != u.ID {
		code, msg := "EMAIL_EXISTS", "该邮箱已被其他账户使用"
		if id.channel == channelSMS {
			code, msg = "PHONE_EXISTS", "该手机号已被其他账户使用"
		}
		common.WriteError(w, http.StatusConflict, code, msg)
		return
	}
	if err := s.codes.Send(r.Context(), id.channel, id.target, purposeBindNew); err != nil {
		writeSendCodeError(w, "contact bind code", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "channel": id.channel, "ttlSeconds": int(s.codes.ttl.Seconds())})
}

type changeContactReq struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
	NewCode     string `json:"newCode"` // code sent to the new email/phone
	OldCode     string `json:"oldCode"` // code (purpose=security) sent to the old email/phone
	TOTPCode    string `json:"totpCode"`
}

// PUT /account/email and PUT /account/phone — change or bind.
func (s *Server) handleChangeContact(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.mustUser(w, r)
		if !ok {
			return
		}
		var req changeContactReq
		if err := common.DecodeJSON(r, &req); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
			return
		}
		var id identity
		var err error
		if kind == channelEmail {
			id, err = resolveIdentity(req.Email, "", "")
		} else {
			if strings.TrimSpace(req.Phone) == "" {
				err = errNoIdentity
			} else {
				id, err = resolveIdentity("", req.Phone, req.CountryCode)
			}
		}
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		if kind == channelSMS && !s.smsAvailable() {
			common.WriteError(w, http.StatusServiceUnavailable, "SMS_UNAVAILABLE", "短信服务暂不可用，请稍后再试")
			return
		}
		if !requireTOTPIfEnabled(w, u, req.TOTPCode) {
			return
		}
		// Old-contact proof: the code goes to the contact being replaced; when
		// binding a new channel it goes to the user's existing contact.
		oldTarget := s.contactTarget(u, kind)
		if oldTarget == "" {
			oldTarget = s.contactTarget(u, s.contactChannel(u))
		}
		if oldTarget == id.target {
			common.WriteError(w, http.StatusBadRequest, "CONTACT_UNCHANGED", "新旧联系方式相同")
			return
		}
		if !s.verifyCodeAt(w, r, oldTarget, purposeSecurity, req.OldCode) {
			return
		}
		if !s.verifyCodeAt(w, r, id.target, purposeBindNew, req.NewCode) {
			return
		}
		var e, p *string
		if kind == channelEmail {
			e = &id.target
		} else {
			p = &id.target
		}
		switch err := s.store.updateContact(r.Context(), u.ID, e, p, id.countryCode); {
		case errors.Is(err, ErrEmailExists):
			common.WriteError(w, http.StatusConflict, "EMAIL_EXISTS", "该邮箱已被其他账户使用")
			return
		case errors.Is(err, ErrPhoneExists):
			common.WriteError(w, http.StatusConflict, "PHONE_EXISTS", "该手机号已被其他账户使用")
			return
		case err != nil:
			internalErr(w)
			return
		}
		what := "修改了绑定邮箱"
		if kind == channelSMS {
			what = "修改了绑定手机"
		}
		// Notify the OLD address too (u still holds the old email).
		s.securityChanged(r.Context(), u, what, true)
		if nu, err := s.store.UserByID(r.Context(), u.ID); err == nil && kind == channelEmail && u.Email.Valid {
			s.securityChanged(r.Context(), nu, what, false)
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ---- fund password ----

var fundPwRe = regexp.MustCompile(`^[0-9]{6}$`)

type fundPwReq struct {
	FundPassword    string `json:"fundPassword"`
	OldFundPassword string `json:"oldFundPassword"`
	TOTPCode        string `json:"totpCode"`
	Code            string `json:"code"`
}

// handleFundPassword: POST set (first time), PUT change, POST /reset reset.
func (s *Server) handleFundPassword(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.mustUser(w, r)
		if !ok {
			return
		}
		var req fundPwReq
		if err := common.DecodeJSON(r, &req); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
			return
		}
		if !fundPwRe.MatchString(req.FundPassword) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "资金密码须为 6 位数字")
			return
		}
		if checkPassword(u.PasswordHash, req.FundPassword) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "资金密码不能与登录密码相同")
			return
		}
		ctx := r.Context()
		si, err := s.store.secInfo(ctx, u.ID)
		if err != nil {
			internalErr(w)
			return
		}
		switch mode {
		case "set":
			if si.FundHash != "" {
				common.WriteError(w, http.StatusConflict, "FUND_PASSWORD_EXISTS", "资金密码已设置，请使用修改或重置")
				return
			}
		case "change":
			if si.FundHash == "" {
				common.WriteError(w, http.StatusBadRequest, "FUND_PASSWORD_NOT_SET", "尚未设置资金密码")
				return
			}
			if !s.checkFund(w, ctx, u, si, req.OldFundPassword) {
				return
			}
		case "reset":
			if si.FundHash == "" {
				common.WriteError(w, http.StatusBadRequest, "FUND_PASSWORD_NOT_SET", "尚未设置资金密码")
				return
			}
		}
		if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
			return
		}
		hash, err := hashPassword(req.FundPassword)
		if err != nil || s.store.setFundPassword(ctx, u.ID, hash) != nil {
			internalErr(w)
			return
		}
		what := map[string]string{"set": "设置了资金密码", "change": "修改了资金密码", "reset": "重置了资金密码"}[mode]
		s.securityChanged(ctx, u, what, mode != "set")
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "fundPasswordSet": true})
	}
}

// checkFund verifies a fund password with lockout; writes the API error.
func (s *Server) checkFund(w http.ResponseWriter, ctx context.Context, u *User, si secInfo, pw string) bool {
	reason, lockedUntil := s.fundVerdict(ctx, u, si, pw)
	switch reason {
	case "":
		return true
	case "fund_password_locked":
		common.WriteJSON(w, http.StatusLocked, map[string]any{"code": "FUND_PASSWORD_LOCKED",
			"message": "资金密码错误次数过多，已暂时锁定", "lockedUntil": lockedUntil})
	case "fund_password_required":
		common.WriteError(w, http.StatusBadRequest, "FUND_PASSWORD_REQUIRED", "请输入资金密码")
	default:
		common.WriteError(w, http.StatusBadRequest, "FUND_PASSWORD_INVALID", "资金密码错误")
	}
	return false
}

// fundVerdict returns "" when pw is correct, else a reason.
func (s *Server) fundVerdict(ctx context.Context, u *User, si secInfo, pw string) (string, *string) {
	now := s.now()
	if lu := timePtr(si.FundLocked, now); lu != nil {
		return "fund_password_locked", lu
	}
	if pw == "" {
		return "fund_password_required", nil
	}
	if checkPassword(si.FundHash, pw) {
		if si.FundFailed > 0 {
			s.store.fundOK(ctx, u.ID)
		}
		return "", nil
	}
	cfg, _ := s.store.SecuritySettings(ctx)
	locked, err := s.store.fundFail(ctx, u.ID, cfg.FundPasswordMaxAttempts, time.Duration(cfg.FundPasswordLockMinutes)*time.Minute, now)
	if err != nil {
		log.Printf("fund password fail count: %v", err)
	}
	if locked {
		v := now.Add(time.Duration(cfg.FundPasswordLockMinutes) * time.Minute).UTC().Format(time.RFC3339)
		s.notifyAsync(u, common.NoticeSecurity, "资金密码已锁定", "资金密码连续输错次数过多，已锁定。如非本人操作，请立即修改登录密码并联系客服。")
		return "fund_password_locked", &v
	}
	return "fund_password_invalid", nil
}

// ---- anti-phishing ----

var antiPhishRe = regexp.MustCompile(`^[A-Za-z0-9]{4,20}$`)

type antiPhishReq struct {
	AntiPhishingCode string `json:"antiPhishingCode"`
	TOTPCode         string `json:"totpCode"`
	Code             string `json:"code"`
}

func (s *Server) handleAntiPhishing(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	var req antiPhishReq
	if err := common.DecodeJSON(r, &req); err != nil || !antiPhishRe.MatchString(req.AntiPhishingCode) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "防钓鱼码须为 4-20 位字母或数字")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	if err := s.store.setAntiPhishing(r.Context(), u.ID, req.AntiPhishingCode); err != nil {
		internalErr(w)
		return
	}
	s.securityChanged(r.Context(), u, "设置了防钓鱼码", false)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "antiPhishingSet": true})
}

// antiPhishingMailer prefixes every outgoing email with the recipient's
// anti-phishing code (when set).
type antiPhishingMailer struct {
	inner EmailSender
	store *Store
	rec   *CodeRecorder
}

func (m antiPhishingMailer) prefix(ctx context.Context, to string) string {
	if m.store == nil {
		return ""
	}
	if c := m.store.antiPhishingByEmail(ctx, to); c != "" {
		return "【防钓鱼码：" + c + "】\n\n"
	}
	return ""
}

func (m antiPhishingMailer) SendEmailCode(ctx context.Context, email, code, purpose string) error {
	p := m.prefix(ctx, email)
	if p == "" {
		return m.inner.SendEmailCode(ctx, email, code, purpose)
	}
	title := purposeSubject[purpose]
	if title == "" {
		title = "验证码"
	}
	body := p + "您的" + title + "为：" + code + "\n\n10 分钟内有效，请勿泄露给任何人。如非本人操作请忽略本邮件。"
	if err := m.inner.SendEmail(ctx, email, codeEmailSubject(title, code), body); err != nil {
		return err
	}
	m.rec.record(email, purpose, code)
	return nil
}

func (m antiPhishingMailer) SendEmail(ctx context.Context, to, subject, text string) error {
	return m.inner.SendEmail(ctx, to, subject, m.prefix(ctx, to)+text)
}

// ---- sessions ----

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	items, err := s.store.listSessions(r.Context(), u.ID)
	if err != nil {
		internalErr(w)
		return
	}
	cur := deviceKeyFor(r, "")
	for i := range items {
		items[i].Current = items[i].DeviceID == cur
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	done, err := s.store.revokeSession(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		internalErr(w)
		return
	}
	if !done {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "会话不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	n, err := s.store.revokeOtherSessions(r.Context(), u.ID, deviceKeyFor(r, ""))
	if err != nil {
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

// ---- internal: outflow verification (wallet) ----

// outflowCheck enforces the withdrawal lock and fund password for an outflow
// action ("withdraw" | "internal_transfer"). Returns "" when allowed.
func (s *Server) outflowCheck(ctx context.Context, u *User, action, fundPw string) (string, *string) {
	if action == "" {
		return "", nil // legacy callers: 2FA only
	}
	cfg, _ := s.store.SecuritySettings(ctx)
	si, err := s.store.secInfo(ctx, u.ID)
	if err != nil {
		return "internal_error", nil
	}
	if action == "withdraw" || cfg.LockAppliesToInternalXfer {
		if lu := timePtr(si.WithdrawLock, s.now()); lu != nil {
			return "withdraw_locked", lu
		}
	}
	need := (action == "withdraw" && cfg.FundPasswordForWithdraw) || (action == "internal_transfer" && cfg.FundPasswordForInternalXfer)
	if !need {
		return "", nil
	}
	if si.FundHash == "" {
		if cfg.FundPasswordMandatory {
			return "fund_password_not_set", nil
		}
		return "", nil
	}
	return s.fundVerdict(ctx, u, si, fundPw)
}

// ---- internal admin: settings + unlock ----

var boolFields = map[string]bool{"fundPasswordForWithdraw": true, "fundPasswordForInternalTransfer": true,
	"fundPasswordMandatory": true, "lockAppliesToInternalTransfer": true}

func (s *Server) handleInternalGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	c, err := s.store.SecuritySettings(r.Context())
	if err != nil {
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, c)
}

func (s *Server) handleInternalPutSecuritySettings(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var m map[string]json.RawMessage
	if err := common.DecodeJSON(r, &m); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	var by string
	_ = json.Unmarshal(m["updatedBy"], &by)
	fields := map[string]string{"withdrawLockHours": "withdraw_lock_hours", "fundPasswordForWithdraw": "fund_password_withdraw",
		"fundPasswordForInternalTransfer": "fund_password_internal_transfer", "fundPasswordMandatory": "fund_password_mandatory",
		"fundPasswordMaxAttempts": "fund_password_max_attempts", "fundPasswordLockMinutes": "fund_password_lock_minutes",
		"lockAppliesToInternalTransfer": "lock_applies_to_internal_transfer"}
	before, _ := s.store.SecuritySettings(r.Context())
	type kv struct{ k, v string }
	var puts []kv
	for jf, key := range fields {
		raw, ok := m[jf]
		if !ok {
			continue
		}
		var b bool
		var n int
		if boolFields[jf] {
			if json.Unmarshal(raw, &b) != nil {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", jf+" 须为布尔值")
				return
			}
			puts = append(puts, kv{key, strconv.FormatBool(b)})
			continue
		}
		if json.Unmarshal(raw, &n) != nil || n < 0 || n > 43200 ||
			(jf == "withdrawLockHours" && n > 720) || (jf == "fundPasswordMaxAttempts" && (n < 1 || n > 20)) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", jf+" 取值超出范围")
			return
		}
		puts = append(puts, kv{key, strconv.Itoa(n)})
	}
	if len(puts) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "没有可更新的字段")
		return
	}
	for _, p := range puts {
		if err := s.store.putSetting(r.Context(), p.k, p.v, by); err != nil {
			internalErr(w)
			return
		}
	}
	after, _ := s.store.SecuritySettings(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

func (s *Server) handleInternalClearWithdrawLock(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	ok, err := s.store.clearWithdrawLock(r.Context(), r.PathValue("uid"))
	if err != nil {
		internalErr(w)
		return
	}
	if !ok {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"after": map[string]any{"uid": r.PathValue("uid"), "withdrawLockedUntil": nil}})
}
