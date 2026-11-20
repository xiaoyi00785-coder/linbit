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

// u2f.go: external security keys (YubiKey, Titan, …). We piggy-back on the
// go-webauthn library but pin the authenticator-attachment to CrossPlatform
// and keep the credentials in their own table (u2f_credentials) so the
// platform-authenticator passkeys and the USB keys stay separately listable
// and deletable from the UI.
//
// Unlike passkeys.go we do not use discoverable login: the user must already
// be identified (either by password-first, or by having an active session)
// before U2F sign-in begins, so we can hand the authenticator the exact
// allowed-credentials list it needs to light up the right key.

const u2fMaxKeys = 10

type u2fUser struct {
	u     *User
	creds []webauthn.Credential
}

func (p u2fUser) WebAuthnID() []byte { return []byte(p.u.UID) }
func (p u2fUser) WebAuthnName() string {
	if p.u.Email.Valid {
		return p.u.Email.String
	}
	if p.u.Phone.Valid {
		return p.u.Phone.String
	}
	return p.u.UID
}
func (p u2fUser) WebAuthnDisplayName() string                { return p.WebAuthnName() }
func (p u2fUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }

// ---- store ----

type u2fRow struct {
	ID         int64
	CredID     string
	Name       string
	Cred       webauthn.Credential
	CreatedAt  time.Time
	LastUsedAt sql.NullTime
}

func (s *Store) listU2F(ctx context.Context, userID int64) ([]u2fRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, credential_id, name, credential, created_at, last_used_at
		FROM u2f_credentials WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []u2fRow
	for rows.Next() {
		var p u2fRow
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

func (s *Store) countU2F(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM u2f_credentials WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

var errU2FExists = errors.New("u2f credential exists")

func (s *Store) insertU2F(ctx context.Context, userID int64, name string, c *webauthn.Credential) (int64, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO u2f_credentials (user_id, credential_id, name, credential) VALUES ($1,$2,$3,$4) RETURNING id`,
		userID, base64.RawURLEncoding.EncodeToString(c.ID), name, raw).Scan(&id)
	if uniqueConstraint(err) != "" {
		return 0, errU2FExists
	}
	return id, err
}

func (s *Store) updateU2FUse(ctx context.Context, userID int64, c *webauthn.Credential) {
	raw, _ := json.Marshal(c)
	_, _ = s.db.ExecContext(ctx, `UPDATE u2f_credentials SET credential=$3, last_used_at=now() WHERE user_id=$1 AND credential_id=$2`,
		userID, base64.RawURLEncoding.EncodeToString(c.ID), raw)
}

func (s *Store) deleteU2F(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM u2f_credentials WHERE user_id=$1 AND id::text=$2`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Server) u2fUserFor(ctx context.Context, u *User) (u2fUser, error) {
	rows, err := s.store.listU2F(ctx, u.ID)
	if err != nil {
		return u2fUser{}, err
	}
	pu := u2fUser{u: u}
	for _, r := range rows {
		pu.creds = append(pu.creds, r.Cred)
	}
	return pu, nil
}

// ---- handlers ----

type u2fDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func (s *Server) handleListU2F(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	rows, err := s.store.listU2F(r.Context(), u.ID)
	if err != nil {
		internalErr(w)
		return
	}
	items := []u2fDTO{}
	for _, p := range rows {
		d := u2fDTO{ID: strconv.FormatInt(p.ID, 10), Name: p.Name, CreatedAt: p.CreatedAt}
		if p.LastUsedAt.Valid {
			t := p.LastUsedAt.Time
			d.LastUsedAt = &t
		}
		items = append(items, d)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "enabled": s.cfg.WebAuthnRPID != ""})
}

type u2fBeginReq struct {
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
}

func (s *Server) handleU2FRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req u2fBeginReq
	if err := common.DecodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	pu, err := s.u2fUserFor(r.Context(), u)
	if err != nil {
		internalErr(w)
		return
	}
	if len(pu.creds) >= u2fMaxKeys {
		common.WriteError(w, http.StatusBadRequest, "U2F_LIMIT", "最多可绑定 10 个安全密钥")
		return
	}
	// Also exclude platform passkeys — a browser should not offer the same
	// credential under both methods.
	excl := make([]protocol.CredentialDescriptor, 0, len(pu.creds))
	for _, c := range pu.creds {
		excl = append(excl, c.Descriptor())
	}
	pk, _ := s.passkeyUserFor(r.Context(), u)
	for _, c := range pk.creds {
		excl = append(excl, c.Descriptor())
	}
	opts, sd, err := wa.BeginRegistration(pu,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			AuthenticatorAttachment: protocol.CrossPlatform,
			UserVerification:        protocol.VerificationDiscouraged,
			ResidentKey:             protocol.ResidentKeyRequirementDiscouraged,
		}),
		webauthn.WithExclusions(excl))
	if err != nil {
		log.Printf("u2f register begin: %v", err)
		internalErr(w)
		return
	}
	tok, err := newRefreshToken()
	if err != nil || s.store.putWebAuthnSession(r.Context(), tok, &u.ID, "u2f_register", sd, s.now().Add(webauthnSessionTTL)) != nil {
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sessionId": tok, "publicKey": opts.Response})
}

type u2fFinishReq struct {
	SessionID  string          `json:"sessionId"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

func (s *Server) handleU2FRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req u2fFinishReq
	if err := common.DecodeJSON(r, &req); err != nil || req.SessionID == "" || len(req.Credential) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	sd, sid, err := s.store.takeWebAuthnSession(r.Context(), req.SessionID, "u2f_register")
	if err != nil || !sid.Valid || sid.Int64 != u.ID {
		common.WriteError(w, http.StatusBadRequest, "U2F_SESSION_INVALID", "安全密钥会话无效或已过期")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "U2F_INVALID", "安全密钥数据无效")
		return
	}
	pu, err := s.u2fUserFor(r.Context(), u)
	if err != nil {
		internalErr(w)
		return
	}
	cred, err := wa.CreateCredential(pu, *sd, parsed)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "U2F_INVALID", "安全密钥校验失败")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Security Key"
	}
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	id, err := s.store.insertU2F(r.Context(), u.ID, name, cred)
	if errors.Is(err, errU2FExists) {
		common.WriteError(w, http.StatusConflict, "U2F_EXISTS", "该安全密钥已绑定")
		return
	}
	if err != nil {
		internalErr(w)
		return
	}
	s.securityChanged(r.Context(), u, "添加了安全密钥「"+name+"」", false)
	common.WriteJSON(w, http.StatusCreated, u2fDTO{ID: strconv.FormatInt(id, 10), Name: name, CreatedAt: s.now()})
}

func (s *Server) handleDeleteU2F(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	done, err := s.store.deleteU2F(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		internalErr(w)
		return
	}
	if !done {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "安全密钥不存在")
		return
	}
	s.securityChanged(r.Context(), u, "删除了安全密钥", false)
	w.WriteHeader(http.StatusNoContent)
}

// ---- login (step-up after password) ----

type u2fAuthBeginReq struct {
	UID string `json:"uid"`
}

// handleU2FAuthBegin issues a challenge for a user who has already passed the
// first factor (password) and has at least one security key bound. It returns
// the allowed-credentials list so the authenticator knows which key to look
// for. For step-up inside an authed session the caller may leave uid empty.
func (s *Server) handleU2FAuthBegin(w http.ResponseWriter, r *http.Request) {
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req u2fAuthBeginReq
	_ = common.DecodeJSON(r, &req)
	uid := strings.TrimSpace(req.UID)
	var u *User
	if uid != "" {
		var err error
		u, err = s.store.UserByUID(r.Context(), uid)
		if err != nil || u == nil {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "用户不存在")
			return
		}
	} else {
		authed, ok := s.authedUser(r)
		if !ok {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		u = authed
	}
	pu, err := s.u2fUserFor(r.Context(), u)
	if err != nil {
		internalErr(w)
		return
	}
	if len(pu.creds) == 0 {
		common.WriteError(w, http.StatusBadRequest, "U2F_NOT_BOUND", "该账户未绑定安全密钥")
		return
	}
	opts, sd, err := wa.BeginLogin(pu, webauthn.WithUserVerification(protocol.VerificationDiscouraged))
	if err != nil {
		internalErr(w)
		return
	}
	tok, err := newRefreshToken()
	if err != nil || s.store.putWebAuthnSession(r.Context(), tok, &u.ID, "u2f_auth", sd, s.now().Add(webauthnSessionTTL)) != nil {
		internalErr(w)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"sessionId": tok, "publicKey": opts.Response})
}

type u2fAuthFinishReq struct {
	SessionID  string          `json:"sessionId"`
	Credential json.RawMessage `json:"credential"`
}

// handleU2FAuthFinish verifies a U2F assertion against a prior begin session.
// It does NOT itself issue tokens — the caller (password login finish, or a
// sensitive-action handler) checks the verified flag and continues its own
// flow. We just record the signature + counter bump.
func (s *Server) handleU2FAuthFinish(w http.ResponseWriter, r *http.Request) {
	wa, ok := s.mustWebAuthn(w)
	if !ok {
		return
	}
	var req u2fAuthFinishReq
	if err := common.DecodeJSON(r, &req); err != nil || req.SessionID == "" || len(req.Credential) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	sd, sid, err := s.store.takeWebAuthnSession(r.Context(), req.SessionID, "u2f_auth")
	if err != nil || !sid.Valid {
		common.WriteError(w, http.StatusBadRequest, "U2F_SESSION_INVALID", "安全密钥会话无效或已过期")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "U2F_INVALID", "安全密钥数据无效")
		return
	}
	u, err := s.store.UserByID(r.Context(), sid.Int64)
	if err != nil || u == nil {
		common.WriteError(w, http.StatusUnauthorized, "U2F_INVALID", "安全密钥校验失败")
		return
	}
	pu, err := s.u2fUserFor(r.Context(), u)
	if err != nil {
		internalErr(w)
		return
	}
	cred, err := wa.ValidateLogin(pu, *sd, parsed)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "U2F_INVALID", "安全密钥校验失败")
		return
	}
	s.store.updateU2FUse(r.Context(), u.ID, cred)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "uid": u.UID})
}
