package main

// 角色编辑（RBAC 可配置）。
//
// 内置角色的默认权限在 rbac.go（rolePerms）；admin_roles 表里的行覆盖它们，
// 也可新增自定义角色。super_admin 不可编辑。lockedPerms 里的资金/冻结类权限点
// 由代码强制保持默认：内置角色始终保留默认里的锁定权限点，自定义角色永远拿不到。
// 这样角色编辑不会改变谁能动资金或冻结账户。

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/linbit/platform/services/go/common"
)

var lockedPerms = map[string]bool{
	permWithdrawFirst: true, permWithdrawSecond: true, permFundAdjust: true, permFundAdjustApprove: true,
	permFeeEdit: true, permFeeApprove: true, permRewardApprove: true, permCouponApprove: true,
	permEarnEdit: true, permUserEdit: true, permMMEdit: true, permAdminManage: true,
	permWalletColdSweep: true,
}

var builtinRoleNames = map[string]string{
	roleSuper: "超级管理员", roleOperations: "运营", roleKYCReviewer: "KYC 审核", roleFinanceFirst: "财务初审",
	roleFinanceSecond: "财务复审", roleRisk: "风控", roleSupport: "客服", roleMarketing: "市场", roleAuditor: "审计（只读）",
}

type roleMeta struct {
	Name      string
	Custom    bool
	Override  bool
	UpdatedBy string
	UpdatedAt time.Time
}

type roleState struct {
	perms map[string][]string
	meta  map[string]roleMeta
}

var roleReg atomic.Pointer[roleState]

func defaultRoleState() *roleState {
	st := &roleState{perms: map[string][]string{}, meta: map[string]roleMeta{}}
	for k, v := range rolePerms {
		st.perms[k] = append([]string(nil), v...)
		st.meta[k] = roleMeta{Name: builtinRoleNames[k]}
	}
	return st
}

// effectiveRoles is the live role → perms matrix (defaults until loadRoles runs).
func effectiveRoles() map[string][]string {
	if st := roleReg.Load(); st != nil {
		return st.perms
	}
	return rolePerms
}

var permOrder = func() map[string]int {
	m := map[string]int{}
	for i, p := range allPerms {
		m[p] = i
	}
	return m
}()

func sortPerms(ps []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range ps {
		if _, ok := permOrder[p]; ok && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return permOrder[out[i]] < permOrder[out[j]] })
	return out
}

// finalPerms applies the lock: requested non-locked perms + (for built-in roles)
// the locked perms of the code default.
func finalPerms(code string, req []string) []string {
	out := []string{}
	for _, p := range req {
		if !lockedPerms[p] {
			out = append(out, p)
		}
	}
	for _, p := range rolePerms[code] {
		if lockedPerms[p] {
			out = append(out, p)
		}
	}
	return sortPerms(out)
}

func (s *Server) loadRoles(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT code, name, perms, updated_by, updated_at FROM admin_roles`)
	if err != nil {
		return err
	}
	defer rows.Close()
	st := defaultRoleState()
	for rows.Next() {
		var code, name, by string
		var perms []string
		var at time.Time
		if err := rows.Scan(&code, &name, (*textArray)(&perms), &by, &at); err != nil {
			return err
		}
		if code == roleSuper {
			continue
		}
		_, builtin := rolePerms[code]
		st.perms[code] = finalPerms(code, perms)
		m := roleMeta{Name: name, Custom: !builtin, Override: builtin, UpdatedBy: by, UpdatedAt: at}
		if m.Name == "" {
			m.Name = builtinRoleNames[code]
		}
		st.meta[code] = m
	}
	if err := rows.Err(); err != nil {
		return err
	}
	roleReg.Store(st)
	return nil
}

// RunRoleSync reloads the role table periodically (and once at startup).
func (s *Server) RunRoleSync(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.loadRoles(ctx); err != nil {
				log.Printf("roles: reload failed: %v", err)
			}
		}
	}
}

func (s *Server) registerRoleRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/roles", permAdminManage, s.handleListRoles)
	a("POST /admin/v1/roles", permAdminManage, s.handleCreateRole)
	a("PUT /admin/v1/roles/{code}", permAdminManage, s.handlePutRole)
	a("DELETE /admin/v1/roles/{code}", permAdminManage, s.handleDeleteRole)
}

type roleDTO struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Builtin      bool     `json:"builtin"`
	Editable     bool     `json:"editable"`
	Overridden   bool     `json:"overridden"`
	Perms        []string `json:"perms"`
	DefaultPerms []string `json:"defaultPerms,omitempty"`
	Members      int      `json:"members"`
	UpdatedBy    string   `json:"updatedBy,omitempty"`
	UpdatedAt    string   `json:"updatedAt,omitempty"`
}

func (s *Server) roleDTOs(ctx context.Context) ([]roleDTO, error) {
	members := map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT r, count(*) FROM admin_users, unnest(roles) r WHERE status = 'active' GROUP BY r`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r string
		var n int
		if rows.Scan(&r, &n) == nil {
			members[r] = n
		}
	}
	rows.Close()
	st := roleReg.Load()
	if st == nil {
		st = defaultRoleState()
	}
	out := []roleDTO{}
	for code, perms := range st.perms {
		m := st.meta[code]
		_, builtin := rolePerms[code]
		d := roleDTO{Code: code, Name: m.Name, Builtin: builtin, Editable: code != roleSuper, Overridden: m.Override,
			Perms: sortPerms(perms), Members: members[code], UpdatedBy: m.UpdatedBy}
		if builtin {
			d.DefaultPerms = sortPerms(rolePerms[code])
		}
		if !m.UpdatedAt.IsZero() {
			d.UpdatedAt = fmtTime(m.UpdatedAt)
		}
		out = append(out, d)
	}
	order := []string{roleSuper, roleOperations, roleKYCReviewer, roleFinanceFirst, roleFinanceSecond, roleRisk, roleSupport, roleMarketing, roleAuditor}
	idx := map[string]int{}
	for i, c := range order {
		idx[c] = i + 1
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := idx[out[i].Code], idx[out[j].Code]
		if a == 0 && b == 0 {
			return out[i].Code < out[j].Code
		}
		if a == 0 || b == 0 {
			return a != 0
		}
		return a < b
	})
	return out, nil
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	items, err := s.roleDTOs(r.Context())
	if err != nil {
		internalErr(w, "list roles", err)
		return
	}
	locked := []string{}
	for _, p := range allPerms {
		if lockedPerms[p] {
			locked = append(locked, p)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items, "allPerms": allPerms, "lockedPerms": locked})
}

var roleCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

type roleReq struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Perms    []string `json:"perms"`
	TOTPCode string   `json:"totpCode"`
	Reason   string   `json:"reason"`
}

func validPermList(ps []string) bool {
	for _, p := range ps {
		if _, ok := permOrder[p]; !ok {
			return false
		}
	}
	return true
}

func (s *Server) roleSnapshot(code string) map[string]interface{} {
	st := roleReg.Load()
	if st == nil {
		st = defaultRoleState()
	}
	p, ok := st.perms[code]
	if !ok {
		return nil
	}
	return map[string]interface{}{"code": code, "name": st.meta[code].Name, "perms": p}
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req roleReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	req.Code, req.Name = strings.TrimSpace(req.Code), strings.TrimSpace(req.Name)
	if !roleCodeRe.MatchString(req.Code) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "角色代码需为小写字母开头的 2-32 位字母/数字/下划线")
		return
	}
	if _, ok := effectiveRoles()[req.Code]; ok {
		common.WriteError(w, http.StatusConflict, "ROLE_EXISTS", "角色已存在")
		return
	}
	if req.Name == "" || len([]rune(req.Name)) > 32 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写角色名称（≤32 字）")
		return
	}
	if !validPermList(req.Perms) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "包含未知权限点")
		return
	}
	perms := finalPerms(req.Code, req.Perms)
	if len(perms) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "至少选择一个可编辑的权限点")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO admin_roles (code, name, perms, builtin, updated_by) VALUES ($1,$2,$3,false,$4)`,
		req.Code, req.Name, textArray(perms), a.Username); err != nil {
		internalErr(w, "create role", err)
		return
	}
	if err := s.loadRoles(r.Context()); err != nil {
		log.Printf("roles: reload after create: %v", err)
	}
	after := s.roleSnapshot(req.Code)
	s.audit(r, a, "admin", "role_create", "role:"+req.Code, levelHigh, "ok", nil, after, req.Reason)
	common.WriteJSON(w, http.StatusOK, after)
}

func (s *Server) handlePutRole(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	code := r.PathValue("code")
	var req roleReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if code == roleSuper {
		common.WriteError(w, http.StatusBadRequest, "ROLE_IMMUTABLE", "超级管理员角色不可编辑")
		return
	}
	before := s.roleSnapshot(code)
	if before == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "角色不存在")
		return
	}
	if !validPermList(req.Perms) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "包含未知权限点")
		return
	}
	perms := finalPerms(code, req.Perms)
	if len(perms) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "角色至少保留一个权限点")
		return
	}
	_, builtin := rolePerms[code]
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name, _ = before["name"].(string)
	}
	if len([]rune(name)) > 32 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "角色名称 ≤32 字")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO admin_roles (code, name, perms, builtin, updated_by) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, perms = EXCLUDED.perms, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		code, name, textArray(perms), builtin, a.Username); err != nil {
		internalErr(w, "put role", err)
		return
	}
	if err := s.loadRoles(r.Context()); err != nil {
		log.Printf("roles: reload after update: %v", err)
	}
	after := s.roleSnapshot(code)
	s.audit(r, a, "admin", "role_update", "role:"+code, levelHigh, "ok", before, after, req.Reason)
	common.WriteJSON(w, http.StatusOK, after)
}

// DELETE resets a built-in role to its code default, or removes an unused custom role.
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	code := r.PathValue("code")
	body, code2fa, reason, err := writeReq(r)
	_ = body
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if code == roleSuper {
		common.WriteError(w, http.StatusBadRequest, "ROLE_IMMUTABLE", "超级管理员角色不可编辑")
		return
	}
	before := s.roleSnapshot(code)
	if before == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "角色不存在")
		return
	}
	_, builtin := rolePerms[code]
	if !builtin {
		var n int
		if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM admin_users WHERE $1 = ANY(roles)`, code).Scan(&n); err != nil {
			internalErr(w, "delete role count", err)
			return
		}
		if n > 0 {
			common.WriteError(w, http.StatusConflict, "ROLE_IN_USE", "仍有管理员持有该角色，请先移除")
			return
		}
	}
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	if !s.verify2FA(w, a, code2fa) {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM admin_roles WHERE code = $1`, code)
	if err != nil {
		internalErr(w, "delete role", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 && builtin {
		common.WriteError(w, http.StatusBadRequest, "NOTHING_TO_RESET", "该角色已是默认权限")
		return
	}
	if err := s.loadRoles(r.Context()); err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("roles: reload after delete: %v", err)
	}
	action := "role_delete"
	if builtin {
		action = "role_reset"
	}
	after := s.roleSnapshot(code)
	s.audit(r, a, "admin", action, "role:"+code, levelHigh, "ok", before, after, reason)
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "role": after})
}
