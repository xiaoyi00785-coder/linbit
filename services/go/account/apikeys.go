package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Primary-account API keys reuse the sub-account scheme: the gateway sends
// X-LB-APIKEY + X-LB-SECRET, and the account service compares sha256(secret).
// The secret is not an HMAC key, so storing only the hash does not break
// verification. This batch accepts permission "read" only.

const maxUserAPIKeys = 10

func userAPIPerms(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, errors.New("empty")
	}
	for _, p := range in {
		if strings.ToLower(strings.TrimSpace(p)) != "read" {
			return nil, errors.New(p)
		}
	}
	return []string{"read"}, nil
}

type userKeyDTO struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	APIKey      string   `json:"apiKey"`
	Permissions []string `json:"permissions"`
	IPWhitelist []string `json:"ipWhitelist"`
	CreatedAt   int64    `json:"createdAt"`
}

type userAPIKey struct {
	ID          int64
	APIKey      string
	SecretHash  string
	UserID      int64
	UID         string
	Label       string
	Permissions []string
	IPAllowlist []string
	CreatedAt   time.Time
	RevokedAt   sql.NullTime
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT id, api_key, label, ip_allowlist, created_at FROM user_api_keys
		WHERE user_id=$1 AND revoked_at IS NULL ORDER BY id DESC`, u.ID)
	if err != nil {
		log.Printf("list api keys: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []userKeyDTO{}
	for rows.Next() {
		var id int64
		var key, label, ips string
		var at time.Time
		if err := rows.Scan(&id, &key, &label, &ips, &at); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items = append(items, userKeyDTO{ID: strconv.FormatInt(id, 10), Label: label, APIKey: key, Permissions: []string{"read"}, IPWhitelist: splitList(ips), CreatedAt: at.UnixMilli()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req struct {
		Label       string   `json:"label"`
		Permissions []string `json:"permissions"`
		IPWhitelist []string `json:"ipWhitelist"`
		IPAllowlist []string `json:"ipAllowlist"`
		TOTPCode    string   `json:"totpCode"`
		Code        string   `json:"code"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || len([]rune(strings.TrimSpace(req.Label))) == 0 || len([]rune(req.Label)) > 40 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "label 必填且不超过 40 字")
		return
	}
	if _, err := userAPIPerms(req.Permissions); err != nil {
		common.WriteError(w, http.StatusBadRequest, "PERMISSION_NOT_ALLOWED", "本批 API Key 仅允许 read，不能交易或提币")
		return
	}
	ipsIn := req.IPWhitelist
	if len(ipsIn) == 0 {
		ipsIn = req.IPAllowlist
	}
	ips, err := normalizeIPs(ipsIn)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "IP 白名单格式不正确（最多 20 条 IPv4/IPv6 或 CIDR）")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	var n int
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM user_api_keys WHERE user_id=$1 AND revoked_at IS NULL`, u.ID).Scan(&n); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if n >= maxUserAPIKeys {
		common.WriteError(w, http.StatusConflict, "API_KEY_LIMIT", "API Key 数量已达上限")
		return
	}
	keyHex, err1 := randHex(16)
	secret, err2 := randHex(32)
	if err1 != nil || err2 != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	apiKey := "lbu_" + keyHex
	var id int64
	var at time.Time
	err = s.store.db.QueryRowContext(r.Context(), `INSERT INTO user_api_keys (user_id, api_key, secret_hash, label, permissions, ip_allowlist)
		VALUES ($1,$2,$3,$4,'read',$5) RETURNING id, created_at`,
		u.ID, apiKey, sha256Hex(secret), strings.TrimSpace(req.Label), strings.Join(ips, ",")).Scan(&id, &at)
	if err != nil {
		log.Printf("create api key: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	item := userKeyDTO{ID: strconv.FormatInt(id, 10), Label: strings.TrimSpace(req.Label), APIKey: apiKey, Permissions: []string{"read"}, IPWhitelist: ips, CreatedAt: at.UnixMilli()}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"item": item, "secret": secret})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	res, err := s.store.db.ExecContext(r.Context(), `UPDATE user_api_keys SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "API Key 不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

func (s *Server) verifyUserAPIKey(w http.ResponseWriter, r *http.Request, apiKey, secret, ip string) bool {
	if !strings.HasPrefix(apiKey, "lbu_") || s.store == nil {
		return false
	}
	k, err := s.store.UserKeyByAPIKey(r.Context(), apiKey)
	if err != nil || k.RevokedAt.Valid || subtle.ConstantTimeCompare([]byte(k.SecretHash), []byte(sha256Hex(secret))) != 1 {
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "invalid"})
		return true
	}
	if !ipAllowed(k.IPAllowlist, ip) {
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "ip_not_allowed"})
		return true
	}
	u, err := s.store.UserByID(r.Context(), k.UserID)
	if err != nil || u.Status == userFrozen {
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "account_frozen"})
		return true
	}
	_, _ = s.store.db.ExecContext(r.Context(), `UPDATE user_api_keys SET last_used_at=now() WHERE id=$1 AND revoked_at IS NULL`, k.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "uid": u.UID, "parentUid": "", "permissions": []string{"read"}})
	return true
}

func (s *Store) UserKeyByAPIKey(ctx context.Context, apiKey string) (*userAPIKey, error) {
	return scanUserKey(s.db.QueryRowContext(ctx, `SELECT k.id, k.api_key, k.secret_hash, k.user_id, u.uid, k.label, k.permissions, k.ip_allowlist, k.created_at, k.revoked_at
		FROM user_api_keys k JOIN users u ON u.id=k.user_id WHERE k.api_key=$1`, apiKey))
}

func scanUserKey(row *sql.Row) (*userAPIKey, error) {
	var k userAPIKey
	var perms, ips string
	err := row.Scan(&k.ID, &k.APIKey, &k.SecretHash, &k.UserID, &k.UID, &k.Label, &perms, &ips, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		return nil, err
	}
	k.Permissions, k.IPAllowlist = splitList(perms), splitList(ips)
	return &k, nil
}
