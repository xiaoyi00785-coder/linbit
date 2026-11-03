package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// Bootstrap creates the first super admin from env when the table is empty.
func (s *Server) Bootstrap(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM admin_users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 || s.cfg.BootstrapUser == "" {
		if n == 0 {
			log.Printf("admin: no admin accounts and ADMIN_BOOTSTRAP_USERNAME unset; nobody can log in")
		}
		return nil
	}
	if len(s.cfg.BootstrapPass) < s.cfg.MinPasswordLen {
		return errors.New("ADMIN_BOOTSTRAP_PASSWORD too short")
	}
	if s.cfg.Require2FA && s.cfg.BootstrapTOTP == "" {
		return errors.New("ADMIN_BOOTSTRAP_TOTP_SECRET required when ADMIN_REQUIRE_2FA=true")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(s.cfg.BootstrapPass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_users (username, password_hash, totp_secret, roles, created_by)
		VALUES ($1,$2,$3,$4,'bootstrap')`, s.cfg.BootstrapUser, string(h), nullStr(s.cfg.BootstrapTOTP), textArray{roleSuper})
	if err == nil {
		log.Printf("admin: bootstrap super admin %q created", s.cfg.BootstrapUser)
	}
	return err
}

type adminDTO struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Roles       []string `json:"roles"`
	Status      string   `json:"status"`
	TOTPEnabled bool     `json:"totpEnabled"`
	CreatedBy   string   `json:"createdBy,omitempty"`
	CreatedAt   string   `json:"createdAt"`
}

func (s *Server) loadAdmin(ctx context.Context, id int64) (*adminDTO, error) {
	var d adminDTO
	var secret, by sql.NullString
	var created time.Time
	err := s.db.QueryRowContext(ctx, `SELECT id, username, roles, status, totp_secret, created_by, created_at FROM admin_users WHERE id = $1`, id).
		Scan(&d.ID, &d.Username, (*textArray)(&d.Roles), &d.Status, &secret, &by, &created)
	if err != nil {
		return nil, err
	}
	d.TOTPEnabled, d.CreatedBy, d.CreatedAt = secret.String != "", by.String, fmtTime(created)
	return &d, nil
}

func (s *Server) handleListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id FROM admin_users ORDER BY id`)
	if err != nil {
		internalErr(w, "list admins", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	items := []*adminDTO{}
	for _, id := range ids {
		if d, err := s.loadAdmin(r.Context(), id); err == nil {
			items = append(items, d)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func cleanRoles(in []string) ([]string, bool) {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if !validRole(r) {
			return nil, false
		}
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out, len(out) > 0
}

type createAdminReq struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Roles    []string `json:"roles"`
	TOTPCode string   `json:"totpCode"`
}

// handleCreateAdmin creates an operator. A TOTP secret is generated and
// returned exactly once so the new operator can enroll their authenticator.
func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req createAdminReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	roles, ok := cleanRoles(req.Roles)
	if req.Username == "" || len(req.Username) > 64 || strings.ContainsAny(req.Username, " ,{}\"") || !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "用户名或角色无效")
		return
	}
	if len(req.Password) < s.cfg.MinPasswordLen {
		common.WriteError(w, http.StatusBadRequest, "PASSWORD_TOO_WEAK", "密码长度不足")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Linbit Admin", AccountName: req.Username})
	if err != nil {
		internalErr(w, "totp gen", err)
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		internalErr(w, "bcrypt", err)
		return
	}
	var id int64
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO admin_users (username, password_hash, totp_secret, roles, created_by)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (username) DO NOTHING RETURNING id`,
		req.Username, string(h), key.Secret(), textArray(roles), a.Username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusConflict, "ADMIN_EXISTS", "用户名已存在")
		return
	}
	if err != nil {
		internalErr(w, "create admin", err)
		return
	}
	d, _ := s.loadAdmin(r.Context(), id)
	s.audit(r, a, "admin", "create", "admin:"+strconv.FormatInt(id, 10), levelHigh, "ok", nil, d, "")
	common.WriteJSON(w, http.StatusCreated, map[string]interface{}{"admin": d, "totpSecret": key.Secret(), "totpUri": key.URL()})
}

type patchAdminReq struct {
	Roles    []string `json:"roles"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason"`
	TOTPCode string   `json:"totpCode"`
}

// handlePatchAdmin changes roles or disables / re-enables an operator.
// Accounts are never deleted (DESIGN §8).
func (s *Server) handlePatchAdmin(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
		return
	}
	var req patchAdminReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	before, err := s.loadAdmin(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "管理员不存在")
		return
	} else if err != nil {
		internalErr(w, "load admin", err)
		return
	}
	if req.Status != "" && req.Status != "active" && req.Status != "disabled" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 只能是 active/disabled")
		return
	}
	var roles []string
	if req.Roles != nil {
		if roles, ok = cleanRoles(req.Roles); !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "角色无效")
			return
		}
	}
	if id == a.ID && (req.Status == "disabled" || (roles != nil && !hasRole(roles, roleSuper) && hasRole(a.Roles, roleSuper))) {
		common.WriteError(w, http.StatusBadRequest, "CANNOT_MODIFY_SELF", "不能停用自己或移除自己的超级管理员角色")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internalErr(w, "patch admin", err)
		return
	}
	defer tx.Rollback()
	if roles != nil {
		if _, err := tx.ExecContext(r.Context(), `UPDATE admin_users SET roles = $2, updated_at = now() WHERE id = $1`, id, textArray(roles)); err != nil {
			internalErr(w, "patch admin roles", err)
			return
		}
	}
	if req.Status != "" {
		if _, err := tx.ExecContext(r.Context(), `UPDATE admin_users SET status = $2, updated_at = now() WHERE id = $1`, id, req.Status); err != nil {
			internalErr(w, "patch admin status", err)
			return
		}
		if req.Status == "disabled" {
			_, _ = tx.ExecContext(r.Context(), `UPDATE admin_sessions SET revoked = true WHERE admin_id = $1`, id)
		}
	}
	// Keep at least one active super admin.
	var supers int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM admin_users WHERE status = 'active' AND 'super_admin' = ANY(roles)`).Scan(&supers); err != nil {
		internalErr(w, "patch admin count", err)
		return
	}
	if supers == 0 {
		common.WriteError(w, http.StatusBadRequest, "LAST_SUPER_ADMIN", "至少保留一个启用的超级管理员")
		return
	}
	if err := tx.Commit(); err != nil {
		internalErr(w, "patch admin commit", err)
		return
	}
	after, _ := s.loadAdmin(r.Context(), id)
	s.audit(r, a, "admin", "update", "admin:"+strconv.FormatInt(id, 10), levelHigh, "ok", before, after, req.Reason)
	common.WriteJSON(w, http.StatusOK, after)
}
