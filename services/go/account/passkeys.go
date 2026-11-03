package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/linbit/platform/services/go/common"
)

// passkeys.go: WebAuthn passkeys (register / discoverable login / list / delete).
// The relying party is configured via WEBAUTHN_RP_ID / WEBAUTHN_RP_ORIGINS /
// WEBAUTHN_RP_NAME; with no RP id the endpoints answer 503 PASSKEY_UNAVAILABLE.

const webauthnSessionTTL = 5 * time.Minute

type passkeyUser struct {
	u     *User
	creds []webauthn.Credential
}

func (p passkeyUser) WebAuthnID() []byte { return []byte(p.u.UID) }
func (p passkeyUser) WebAuthnName() string {
	if p.u.Email.Valid {
		return p.u.Email.String
	}
	if p.u.Phone.Valid {
		return p.u.Phone.String
	}
	return p.u.UID
}
func (p passkeyUser) WebAuthnDisplayName() string                { return p.WebAuthnName() }
func (p passkeyUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }

func (s *Server) webAuthn() (*webauthn.WebAuthn, error) {
	if s.cfg.WebAuthnRPID == "" {
		return nil, errors.New("passkeys disabled")
	}
	origins := s.cfg.WebAuthnRPOrigins
	if len(origins) == 0 {
		origins = []string{"https://" + s.cfg.WebAuthnRPID}
	}
	return webauthn.New(&webauthn.Config{RPID: s.cfg.WebAuthnRPID, RPDisplayName: s.cfg.WebAuthnRPName, RPOrigins: origins})
}

func (s *Server) mustWebAuthn(w http.ResponseWriter) (*webauthn.WebAuthn, bool) {
	wa, err := s.webAuthn()
	if err != nil {
		common.WriteError(w, http.StatusServiceUnavailable, "PASSKEY_UNAVAILABLE", "通行密钥功能未启用")
		return nil, false
	}
	return wa, true
}

// ---- store ----

type passkeyRow struct {
	ID         int64
	CredID     string
	Name       string
	Cred       webauthn.Credential
	CreatedAt  time.Time
	LastUsedAt sql.NullTime
}

func (s *Store) listPasskeys(ctx context.Context, userID int64) ([]passkeyRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, credential_id, name, credential, created_at, last_used_at
		FROM passkeys WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []passkeyRow
	for rows.Next() {
		var p passkeyRow
		var raw []byte
		if err := rows.Scan(&p.ID, &p.CredID, &p.Name, &raw, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &p.Cred); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) countPasskeys(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM passkeys WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

func (s *Store) insertPasskey(ctx context.Context, userID int64, name string, c *webauthn.Credential) (int64, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO passkeys (user_id, credential_id, name, credential) VALUES ($1,$2,$3,$4) RETURNING id`,
		userID, base64.RawURLEncoding.EncodeToString(c.ID), name, raw).Scan(&id)
	if uniqueConstraint(err) != "" {
		return 0, errPasskeyExists
	}
	return id, err
}

var errPasskeyExists = errors.New("passkey exists")

func (s *Store) updatePasskeyUse(ctx context.Context, userID int64, c *webauthn.Credential) {
	raw, _ := json.Marshal(c)
	_, _ = s.db.ExecContext(ctx, `UPDATE passkeys SET credential=$3, last_used_at=now() WHERE user_id=$1 AND credential_id=$2`,
		userID, base64.RawURLEncoding.EncodeToString(c.ID), raw)
}

func (s *Store) deletePasskey(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE user_id=$1 AND id::text=$2`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) putWebAuthnSession(ctx context.Context, token string, userID *int64, kind string, sd *webauthn.SessionData, exp time.Time) error {
	raw, err := json.Marshal(sd)
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE expires_at < now()`)
	_, err = s.db.ExecContext(ctx, `INSERT INTO webauthn_sessions (token_hash, user_id, kind, data, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		hashToken(token), userID, kind, raw, exp)
	return err
}

// takeWebAuthnSession consumes (single use) a ceremony session.
func (s *Store) takeWebAuthnSession(ctx context.Context, token, kind string) (*webauthn.SessionData, sql.NullInt64, error) {
	var raw []byte
	var uid sql.NullInt64
	err := s.db.QueryRowContext(ctx, `DELETE FROM webauthn_sessions WHERE token_hash=$1 AND kind=$2 AND expires_at > now()
		RETURNING user_id, data`, hashToken(token), kind).Scan(&uid, &raw)
	if err != nil {
		return nil, uid, err
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal(raw, &sd); err != nil {
		return nil, uid, err
	}
	return &sd, uid, nil
}

func (s *Server) passkeyUserFor(ctx context.Context, u *User) (passkeyUser, error) {
	rows, err := s.store.listPasskeys(ctx, u.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	pu := passkeyUser{u: u}
	for _, r := range rows {
		pu.creds = append(pu.creds, r.Cred)
	}
	return pu, nil
}

// ---- handlers ----

type passkeyDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	rows, err := s.store.listPasskeys(r.Context(), u.ID)
	if err != nil {
		internalErr(w)
		return
	}
	items := []passkeyDTO{}
	for _, p := range rows {
		d := passkeyDTO{ID: strconv.FormatInt(p.ID, 10), Name: p.Name, CreatedAt: p.CreatedAt}
		if p.LastUsedAt.Valid {
			t := p.LastUsedAt.Time
			d.LastUsedAt = &t
		}
		items = append(items, d)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "enabled": s.cfg.WebAuthnRPID != ""})
}

type passkeyBeginReq struct {
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
}

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req passkeyBeginReq
	if err := common.DecodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	pu, err := s.passkeyUserFor(r.Context(), u)
	if err != nil {
		internalErr(w)
		return
	}
	if len(pu.creds) >= 10 {
		common.WriteError(w, http.StatusBadRequest, "PASSKEY_LIMIT", "最多可绑定 10 个通行密钥")
		return
	}
	excl := make([]protocol.CredentialDescriptor, 0, len(pu.creds))
	for _, c := range pu.creds {
		excl = append(excl, c.Descriptor())
	}
	opts, sd, err := wa.BeginRegistration(pu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(excl))
	if err != nil {
		log.Printf("passkey register begin: %v", err)
		internalErr(w)
		return
	}
	tok, err := newRefreshToken()
	if err != nil || s.store.putWebAuthnSession(r.Context(), tok, &u.ID, "register", sd, s.now().Add(webauthnSessionTTL)) != nil {
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sessionId": tok, "publicKey": opts.Response})
}

type passkeyFinishReq struct {
	SessionID  string          `json:"sessionId"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req passkeyFinishReq
	if err := common.DecodeJSON(r, &req); err != nil || req.SessionID == "" || len(req.Credential) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	sd, sid, err := s.store.takeWebAuthnSession(r.Context(), req.SessionID, "register")
	if err != nil || !sid.Valid || sid.Int64 != u.ID {
		common.WriteError(w, http.StatusBadRequest, "PASSKEY_SESSION_INVALID", "通行密钥会话无效或已过期")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "PASSKEY_INVALID", "通行密钥数据无效")
		return
	}
	pu, err := s.passkeyUserFor(r.Context(), u)
	if err != nil {
		internalErr(w)
		return
	}
	cred, err := wa.CreateCredential(pu, *sd, parsed)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "PASSKEY_INVALID", "通行密钥校验失败")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	id, err := s.store.insertPasskey(r.Context(), u.ID, name, cred)
	if errors.Is(err, errPasskeyExists) {
		common.WriteError(w, http.StatusConflict, "PASSKEY_EXISTS", "该通行密钥已绑定")
		return
	}
	if err != nil {
		internalErr(w)
		return
	}
	s.securityChanged(r.Context(), u, "添加了通行密钥「"+name+"」", false)
	common.WriteJSON(w, http.StatusCreated, passkeyDTO{ID: strconv.FormatInt(id, 10), Name: name, CreatedAt: s.now()})
}

func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	done, err := s.store.deletePasskey(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		internalErr(w)
		return
	}
	if !done {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "通行密钥不存在")
		return
	}
	s.securityChanged(r.Context(), u, "删除了通行密钥", false)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	opts, sd, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		internalErr(w)
		return
	}
	tok, err := newRefreshToken()
	if err != nil || s.store.putWebAuthnSession(r.Context(), tok, nil, "login", sd, s.now().Add(webauthnSessionTTL)) != nil {
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sessionId": tok, "publicKey": opts.Response})
}

type passkeyLoginReq struct {
	SessionID  string          `json:"sessionId"`
	Credential json.RawMessage `json:"credential"`
	DeviceID   string          `json:"deviceId"`
	DeviceName string          `json:"deviceName"`
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req passkeyLoginReq
	if err := common.DecodeJSON(r, &req); err != nil || req.SessionID == "" || len(req.Credential) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	sd, _, err := s.store.takeWebAuthnSession(r.Context(), req.SessionID, "login")
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "PASSKEY_SESSION_INVALID", "通行密钥会话无效或已过期")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "PASSKEY_INVALID", "通行密钥数据无效")
		return
	}
	var found *User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.store.UserByUID(r.Context(), string(userHandle))
		if err != nil {
			return nil, err
		}
		found = u
		return s.passkeyUserFor(r.Context(), u)
	}
	cred, err := wa.ValidateDiscoverableLogin(handler, *sd, parsed)
	if err != nil || found == nil {
		common.WriteError(w, http.StatusUnauthorized, "PASSKEY_INVALID", "通行密钥校验失败")
		return
	}
	s.store.updatePasskeyUse(r.Context(), found.ID, cred)
	name := req.DeviceName
	if strings.TrimSpace(name) == "" {
		name = "通行密钥登录"
	}
	dev := deviceFrom(r, req.DeviceID, name)
	known, _ := s.store.DeviceExists(r.Context(), found.ID, dev.DeviceKey)
	_ = s.store.UpsertDevice(r.Context(), found.ID, dev, s.now())
	if !known {
		s.notifyAsync(found, common.NoticeSecurity, "新设备登录提醒",
			"您的账户于 "+s.now().UTC().Format("2006-01-02 15:04:05")+" (UTC) 在新设备「"+dev.Name+"」通过通行密钥登录，IP "+dev.IP+"。如非本人操作，请立即修改密码并联系客服。")
	}
	tokens, err := s.issueTokens(r.Context(), found, dev.DeviceKey)
	if err != nil {
		if errors.Is(err, ErrAccountFrozen) {
			writeFrozen(w)
			return
		}
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, tokens)
}
