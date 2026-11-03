package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

const (
	userActive = "active"
	userFrozen = "frozen"
)

// ErrAccountFrozen is returned when a frozen account tries to obtain tokens.
var ErrAccountFrozen = errors.New("account frozen")

func writeFrozen(w http.ResponseWriter) {
	common.WriteError(w, http.StatusForbidden, "ACCOUNT_FROZEN", "账户已被冻结，请联系客服")
}

// adminUserDTO is the back-office view of a user. Contact fields are returned in
// full; the admin service is responsible for masking them per RBAC.
type adminUserDTO struct {
	UID           string  `json:"uid"`
	Email         *string `json:"email"`
	Phone         *string `json:"phone"`
	CountryCode   string  `json:"countryCode"`
	Nickname      *string `json:"nickname"`
	KYCLevel      int     `json:"kycLevel"`
	TOTPEnabled   bool    `json:"totpEnabled"`
	EmailVerified bool    `json:"emailVerified"`
	PhoneVerified bool    `json:"phoneVerified"`
	Status        string  `json:"status"`
	StatusReason  string  `json:"statusReason"`
	CreatedAt     string  `json:"createdAt"`
}

func toAdminUserDTO(u *User) adminUserDTO {
	ns := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		s := v.String
		return &s
	}
	return adminUserDTO{
		UID: u.UID, Email: ns(u.Email), Phone: ns(u.Phone), CountryCode: u.CountryCode,
		Nickname: ns(u.Nickname), KYCLevel: u.KYCLevel, TOTPEnabled: u.TOTPEnabled,
		EmailVerified: u.EmailVerified, PhoneVerified: u.PhoneVerified,
		Status: u.Status, StatusReason: u.StatusReason,
		CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ListUsers searches users for the back office. q matches uid exactly or an
// email/phone substring; status filters by account status.
func (s *Store) ListUsers(ctx context.Context, q, status string, page, size int) ([]User, int, error) {
	where := []string{"TRUE"}
	args := []interface{}{}
	if q = strings.TrimSpace(q); q != "" {
		args = append(args, q, "%"+strings.ToLower(q)+"%")
		where = append(where, "(uid = $1 OR email LIKE $2 OR phone LIKE $2)")
	}
	if status != "" {
		args = append(args, status)
		where = append(where, "status = $"+strconv.Itoa(len(args)))
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users WHERE `+cond+
		` ORDER BY id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.UID, &u.Email, &u.Phone, &u.CountryCode, &u.PasswordHash,
			&u.Nickname, &u.AvatarURL, &u.KYCLevel, &u.TOTPSecret, &u.TOTPEnabled,
			&u.EmailVerified, &u.PhoneVerified, &u.WithdrawWhitelistEnabled, &u.CreatedAt,
			&u.Status, &u.StatusReason); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// SetStatus changes a user's account status. Freezing also revokes every
// refresh token so existing sessions cannot be renewed.
func (s *Store) SetStatus(ctx context.Context, userID int64, status, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET status = $2, status_reason = $3, status_changed_at = now() WHERE id = $1`,
		userID, status, reason); err != nil {
		return err
	}
	if status == userFrozen {
		if _, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func atoiDefault(v string, def, max int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// GET /internal/admin/users?q=&status=&page=&pageSize=
func (s *Server) handleInternalAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	if status != "" && status != userActive && status != userFrozen {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 不合法")
		return
	}
	page := atoiDefault(q.Get("page"), 1, 1_000_000)
	size := atoiDefault(q.Get("pageSize"), 20, 100)
	users, total, err := s.store.ListUsers(r.Context(), q.Get("q"), status, page, size)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]adminUserDTO, 0, len(users))
	for i := range users {
		items = append(items, toAdminUserDTO(&users[i]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total, "page": page, "pageSize": size})
}

// GET /internal/admin/users/{uid}
func (s *Server) handleInternalAdminUser(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	u, err := s.store.UserByUID(r.Context(), r.PathValue("uid"))
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, toAdminUserDTO(u))
}

type adminStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// POST /internal/admin/users/{uid}/status  {status, reason}
func (s *Server) handleInternalAdminSetStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var req adminStatusRequest
	if err := common.DecodeJSON(r, &req); err != nil || (req.Status != userActive && req.Status != userFrozen) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 必须为 active 或 frozen")
		return
	}
	u, err := s.store.UserByUID(r.Context(), r.PathValue("uid"))
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	if err := s.store.SetStatus(r.Context(), u.ID, req.Status, strings.TrimSpace(req.Reason)); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	u, _ = s.store.UserByUID(r.Context(), u.UID)
	common.WriteJSON(w, http.StatusOK, toAdminUserDTO(u))
}
