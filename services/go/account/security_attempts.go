package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// security_attempts.go: brute-force and replay protection for the second factor
// and for password login. Both counters live on users (see migration 00018), so
// the limit is per account and shared by every account-service replica.

const (
	// totpMaxFailures wrong TOTP codes before the account's TOTP is locked.
	totpMaxFailures = 5
	// totpLockFor is how long a TOTP lock lasts.
	totpLockFor = 15 * time.Minute

	// loginMaxFailures wrong passwords before password login is locked.
	loginMaxFailures = 5
	// loginLockFor is how long a password lock lasts.
	loginLockFor = 15 * time.Minute
)

// totpPeriod is the RFC 6238 time step used by the authenticator apps we
// support. totp_step = unix_seconds / totpPeriod.
const totpPeriod = 30

// totpVerdict is the outcome of verifying a TOTP code.
type totpVerdict int

const (
	totpOK totpVerdict = iota
	// totpInvalid is a wrong or expired code (counts towards the lock).
	totpInvalid
	// totpLocked is "too many wrong codes" — no code is checked at all.
	totpLocked
	// totpReplayed is a code that was already accepted once.
	totpReplayed
)

// totpMatch finds the 30-second step that produced code, accepting one step of
// clock skew in either direction. ok is false when no step matches.
func totpMatch(secret, code string, now time.Time) (step int64, ok bool) {
	opts := totp.ValidateOpts{Period: totpPeriod, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	for _, skew := range []time.Duration{-totpPeriod * time.Second, 0, totpPeriod * time.Second} {
		t := now.Add(skew)
		if ok, err := totp.ValidateCustom(code, secret, t, opts); err == nil && ok {
			return t.Unix() / totpPeriod, true
		}
	}
	return 0, false
}

// verifyTOTP checks code for u with a per-account attempt limit and a replay
// guard, and returns the verdict. It is the only place a TOTP code should be
// verified (login, sensitive actions, withdrawals, disable); the raw
// validateTOTP helper is for generating/checking freshly bound secrets only.
//
// A correct code is consumed even if the caller then rejects the request for
// another reason; the user simply waits for the next 30-second code. Callers
// that can fail afterwards should run this check last.
func (s *Server) verifyTOTP(ctx context.Context, u *User, code string) totpVerdict {
	return s.checkTOTPCode(ctx, u, code, true)
}

// checkTOTP is verifyTOTP without consuming the code: the attempt limit and
// lock still apply, but a correct code stays usable. Used when enabling TOTP,
// where a later check (the email/SMS security code) can still fail — consuming
// first would make the user wait for the next 30-second code to retry — and
// where the replay state is reset on success anyway.
func (s *Server) checkTOTP(ctx context.Context, u *User, code string) totpVerdict {
	return s.checkTOTPCode(ctx, u, code, false)
}

func (s *Server) checkTOTPCode(ctx context.Context, u *User, code string, consume bool) totpVerdict {
	now := s.now()
	lockedUntil, err := s.store.TOTPLockedUntil(ctx, u.ID)
	if err != nil {
		log.Printf("totp: attempt state: %v", err)
		return totpInvalid
	}
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		return totpLocked
	}
	step, ok := totpMatch(u.TOTPSecret, code, now)
	if !ok {
		if _, err := s.store.RecordTOTPFailure(ctx, u.ID, totpMaxFailures, totpLockFor, now); err != nil {
			log.Printf("totp: record failure: %v", err)
		}
		return totpInvalid
	}
	if !consume {
		return totpOK
	}
	// Claim the step atomically: the update only matches when this step is newer
	// than the last accepted one, so the same code cannot be used twice — even
	// by two concurrent requests or across replicas.
	claimed, err := s.store.ClaimTOTPStep(ctx, u.ID, step)
	if err != nil {
		log.Printf("totp: claim step: %v", err)
		return totpInvalid
	}
	if !claimed {
		return totpReplayed
	}
	return totpOK
}

// writeTOTPVerdict answers a failed verifyTOTP with the right API error and
// reports whether verification succeeded.
func writeTOTPVerdict(w http.ResponseWriter, v totpVerdict) bool {
	switch v {
	case totpOK:
		return true
	case totpLocked:
		common.WriteError(w, http.StatusTooManyRequests, "TOTP_LOCKED", "验证码错误次数过多，两步验证已暂时锁定，请 15 分钟后再试")
	case totpReplayed:
		common.WriteError(w, http.StatusBadRequest, "TOTP_REPLAYED", "该验证码已使用，请等待验证器显示新的验证码")
	default:
		common.WriteError(w, http.StatusUnauthorized, "INVALID_TOTP", "两步验证码错误")
	}
	return false
}

// ---- session revocation (A5) ----

// revocationTTL keeps a cutoff at least as long as any access token can live.
func (s *Server) revocationTTL() time.Duration { return s.cfg.AccessTTL + time.Minute }

// revokeUserSessions stops every session of a user immediately: refresh tokens
// are revoked and every access token issued so far is past the user's cutoff.
// Used on freeze, password reset and password change.
func (s *Server) revokeUserSessions(ctx context.Context, u *User) {
	if err := s.store.RevokeAllRefreshTokens(ctx, u.ID); err != nil {
		log.Printf("revoke sessions: refresh tokens: %v", err)
	}
	if err := common.RevokeAccessTokens(ctx, s.rdb, u.UID, s.now(), s.revocationTTL()); err != nil {
		log.Printf("revoke sessions: access tokens: %s", common.RedactSecrets(err.Error()))
	}
}

// revokeDeviceSessions kicks one device: its refresh tokens are revoked and the
// access tokens carrying its device claim stop working.
func (s *Server) revokeDeviceSessions(ctx context.Context, uid string, userID int64, deviceKey string) {
	if err := s.store.RevokeDeviceRefreshTokens(ctx, userID, deviceKey); err != nil {
		log.Printf("revoke device: refresh tokens: %v", err)
	}
	if err := common.RevokeDeviceAccessTokens(ctx, s.rdb, uid, common.DeviceClaim(deviceKey), s.now(), s.revocationTTL()); err != nil {
		log.Printf("revoke device: access tokens: %s", common.RedactSecrets(err.Error()))
	}
}

// ---- password login lock ----

// loginLocked reports whether password login is locked for userID.
func (s *Server) loginLocked(ctx context.Context, userID int64) (bool, time.Time) {
	until, err := s.store.LoginLockedUntil(ctx, userID)
	if err != nil || !until.Valid {
		return false, time.Time{}
	}
	return until.Time.After(s.now()), until.Time
}

// recordLoginFailure counts a wrong password. It returns the lock expiry when
// this attempt reached the limit, so the caller can tell the user and alert.
func (s *Server) recordLoginFailure(ctx context.Context, userID int64) time.Time {
	until, err := s.store.RecordLoginFailure(ctx, userID, loginMaxFailures, loginLockFor, s.now())
	if err != nil {
		log.Printf("login: record failure: %v", err)
		return time.Time{}
	}
	return until
}

// ---- store ----

// TOTPLockedUntil returns the TOTP lock expiry for a user (invalid = not locked).
func (s *Store) TOTPLockedUntil(ctx context.Context, userID int64) (sql.NullTime, error) {
	var until sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT totp_locked_until FROM users WHERE id = $1`, userID).Scan(&until)
	return until, err
}

// ClaimTOTPStep records step as the last accepted TOTP step. It reports false
// when step was already consumed (a replay).
func (s *Store) ClaimTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_last_step = $2, totp_failed = 0, totp_locked_until = NULL
		 WHERE id = $1 AND totp_last_step < $2`, userID, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RecordTOTPFailure increments the TOTP failure counter in one statement and
// sets the lock when the new count reaches max. It reports whether this call
// applied a lock that is still in force.
func (s *Store) RecordTOTPFailure(ctx context.Context, userID int64, max int, lockFor time.Duration, now time.Time) (bool, error) {
	var until sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET
		     totp_locked_until = CASE WHEN totp_failed + 1 >= $2 THEN $3::timestamptz ELSE totp_locked_until END,
		     totp_failed = CASE WHEN totp_failed + 1 >= $2 THEN 0 ELSE totp_failed + 1 END
		 WHERE id = $1
		 RETURNING totp_locked_until`,
		userID, max, now.Add(lockFor)).Scan(&until)
	if err != nil {
		return false, err
	}
	return until.Valid && until.Time.After(now), nil
}

// LoginLockedUntil returns the account's password-login lock expiry.
func (s *Store) LoginLockedUntil(ctx context.Context, userID int64) (sql.NullTime, error) {
	var until sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT login_locked_until FROM users WHERE id = $1`, userID).Scan(&until)
	return until, err
}

// RecordLoginFailure increments the consecutive-failure counter and, when it
// reaches max, sets the lock for lockFor and resets the counter. It returns the
// lock expiry only when a lock is in force (zero time otherwise).
func (s *Store) RecordLoginFailure(ctx context.Context, userID int64, max int, lockFor time.Duration, now time.Time) (time.Time, error) {
	var until sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET
		     login_locked_until = CASE WHEN login_failed + 1 >= $2 THEN $3::timestamptz ELSE login_locked_until END,
		     login_failed = CASE WHEN login_failed + 1 >= $2 THEN 0 ELSE login_failed + 1 END
		 WHERE id = $1
		 RETURNING login_locked_until`,
		userID, max, now.Add(lockFor)).Scan(&until)
	if err != nil || !until.Valid || !until.Time.After(now) {
		return time.Time{}, err
	}
	return until.Time, nil
}

// ClearLoginFailures resets the consecutive password failures after a success.
func (s *Store) ClearLoginFailures(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET login_failed = 0, login_locked_until = NULL WHERE id = $1`, userID)
	return err
}
