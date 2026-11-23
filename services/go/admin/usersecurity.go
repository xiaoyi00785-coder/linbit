package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// GET /admin/v1/users/{uid}/security — 只读：提现锁 / 资金密码锁 / 2FA 锁状态（不含任何密钥或哈希）。
func (s *Server) handleUserSecurity(w http.ResponseWriter, r *http.Request) {
	m, err := s.readOnlyMaps(r.Context(), "2s", `SELECT uid, withdraw_locked_until AS "withdrawLockedUntil", withdraw_lock_reason AS "withdrawLockReason",
		(withdraw_locked_until IS NOT NULL AND withdraw_locked_until > now()) AS "withdrawLocked",
		fund_pw_locked_until AS "fundPasswordLockedUntil", totp_locked_until AS "totpLockedUntil", totp_enabled AS "totpEnabled",
		(COALESCE(fund_password_hash,'') <> '') AS "fundPasswordSet"
		FROM users WHERE uid = $1`, r.PathValue("uid"))
	if err != nil {
		internalErr(w, "user security", err)
		return
	}
	if len(m) == 0 {
		common.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "用户不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, m[0])
}
