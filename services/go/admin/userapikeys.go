package main

// 用户 API Key 管理（只读列表 + 吊销）。
// 只返回脱敏后的 Key 标识（SQL 内截断），从不查询 secret_hash。
// 吊销直接写 user_api_keys.revoked_at：网关每次请求都经 account 的
// /internal/api-keys/verify 校验 revoked_at，无缓存，吊销立即生效。

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

const apiKeyCols = `k.id, u.uid, (left(k.api_key, 6) || '…' || right(k.api_key, 4)) AS "keyMasked",
	k.label, k.permissions, k.ip_allowlist AS "ipAllowlist", k.created_at AS "createdAt", k.last_used_at AS "lastUsedAt",
	k.revoked_at AS "revokedAt", (u.parent_user_id IS NOT NULL) AS "isSub"`

func (s *Server) registerAPIKeyRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/api-keys", permUserView, s.handleListAPIKeys)
	a("POST /admin/v1/api-keys/{id}/revoke", permUserEdit, s.handleRevokeAPIKey)
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	if uid := strings.TrimSpace(r.URL.Query().Get("uid")); uid != "" {
		f.add("u.uid::text = ?", uid)
	}
	switch r.URL.Query().Get("status") {
	case "active":
		f.where = append(f.where, "k.revoked_at IS NULL")
	case "revoked":
		f.where = append(f.where, "k.revoked_at IS NOT NULL")
	}
	if r.URL.Query().Get("noIp") == "1" {
		f.where = append(f.where, "k.ip_allowlist = ''")
	}
	stats := map[string]interface{}{}
	if m, err := s.queryMaps(r.Context(), `SELECT count(*) FILTER (WHERE revoked_at IS NULL) AS active,
		count(*) FILTER (WHERE revoked_at IS NOT NULL) AS revoked,
		count(DISTINCT user_id) FILTER (WHERE revoked_at IS NULL) AS users,
		count(*) FILTER (WHERE revoked_at IS NULL AND ip_allowlist = '') AS "noIp",
		count(*) FILTER (WHERE revoked_at IS NULL AND last_used_at > now() - interval '24 hours') AS "used24h"
		FROM user_api_keys`); err == nil && len(m) > 0 {
		stats = m[0]
	}
	s.pagedList(w, r, "api keys", apiKeyCols, `user_api_keys k JOIN users u ON u.id = k.user_id`, "k.id DESC", f, map[string]interface{}{"stats": stats})
}

func (s *Server) loadAPIKey(r *http.Request, id int64) (map[string]interface{}, error) {
	m, err := s.queryMaps(r.Context(), `SELECT `+apiKeyCols+` FROM user_api_keys k JOIN users u ON u.id = k.user_id WHERE k.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, sql.ErrNoRows
	}
	return m[0], nil
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
		return
	}
	_, code, reason, err := writeReq(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	before, err := s.loadAPIKey(r, id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "API Key 不存在")
		return
	} else if err != nil {
		internalErr(w, "load api key", err)
		return
	}
	if before["revokedAt"] != nil {
		common.WriteError(w, http.StatusConflict, "ALREADY_REVOKED", "该 API Key 已吊销")
		return
	}
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	if !s.verify2FA(w, a, code) {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE user_api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id); err != nil {
		internalErr(w, "revoke api key", err)
		return
	}
	after, _ := s.loadAPIKey(r, id)
	s.audit(r, a, "user", "api_key_revoke", "apikey:"+strconv.FormatInt(id, 10)+" user:"+toStr(before["uid"]), levelHigh, "ok", before, after, reason)
	common.WriteJSON(w, http.StatusOK, after)
}

func toStr(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
