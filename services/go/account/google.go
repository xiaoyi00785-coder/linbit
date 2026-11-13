package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/linbit/platform/services/go/common"
)

// Google sign-in exchanges a Google ID token. It stays unavailable until
// GOOGLE_OAUTH_CLIENT_IDS is set. This file does not seed users.

type googleIdentity struct {
	Sub           string
	Email         string
	EmailVerified bool
}

type googleJWKCache struct {
	mu   sync.Mutex
	at   time.Time
	keys map[string]*rsa.PublicKey
}

func (s *Server) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	ids := s.cfg.GoogleClientIDs
	if ids == nil {
		ids = []string{}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"providers": []map[string]any{{
		"id": "google", "configured": len(s.cfg.GoogleClientIDs) > 0, "clientIds": ids,
	}}})
}

func (s *Server) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if len(s.cfg.GoogleClientIDs) == 0 {
		common.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"code": "GOOGLE_UNCONFIGURED", "message": "Google 登录未配置", "configured": false,
			"provider": "google", "requestId": common.RequestID(),
		})
		return
	}
	var req struct {
		IDToken    string `json:"idToken"`
		DeviceID   string `json:"deviceId"`
		DeviceName string `json:"deviceName"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.IDToken) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "idToken 必填")
		return
	}
	keys, err := s.googleKeys(r.Context())
	if err != nil {
		log.Printf("google jwks: %v", err)
		common.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"code": "GOOGLE_UNAVAILABLE", "message": "暂时无法校验 Google 登录", "configured": true,
			"provider": "google", "requestId": common.RequestID(),
		})
		return
	}
	id, err := verifyGoogleIDToken(req.IDToken, keys, s.cfg.GoogleClientIDs, s.now())
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "GOOGLE_TOKEN_INVALID", "Google 登录凭证无效")
		return
	}
	if s.store == nil || s.store.db == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "账户服务未就绪")
		return
	}
	u, err := s.googleUser(r.Context(), id)
	if err != nil {
		writeGoogleUserErr(w, err)
		return
	}
	// Sub-accounts log in with their own credentials + TOTP only; a Google
	// identity must never become a way into a sub-account session.
	if pid, err := s.store.ParentIDOf(r.Context(), u.ID); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	} else if pid != 0 {
		writeSubLoginDisabled(w)
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if name == "" {
		name = "Google"
	}
	dev := deviceFrom(r, req.DeviceID, name)

	// Google is a first factor only. An account with TOTP gets the same challenge
	// as a password login; completing it is where the TOTP attempt limit and
	// replay guard apply.
	if u.TOTPEnabled {
		challenge, err := newRefreshToken()
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		if err := s.store.StoreLoginChallenge(r.Context(), u.ID, challenge, "totp", dev, s.now().Add(loginChallengeTTL)); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		common.WriteJSON(w, http.StatusOK, loginChallengeResponse{
			ChallengeToken: challenge,
			Methods:        []string{"totp"},
			ExpiresIn:      int(loginChallengeTTL.Seconds()),
		})
		return
	}

	if err := s.store.UpsertDevice(r.Context(), u.ID, dev, s.now()); err != nil {
		log.Printf("google device: %v", err)
	}
	tokens, err := s.issueTokens(r.Context(), u, dev.DeviceKey)
	if err != nil {
		if errors.Is(err, ErrAccountFrozen) {
			writeFrozen(w)
			return
		}
		log.Printf("google tokens: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, tokens)
}

func writeGoogleUserErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errGoogleEmailUnverified):
		common.WriteError(w, http.StatusConflict, "GOOGLE_EMAIL_UNVERIFIED", "该邮箱尚未在本站完成验证，不能自动绑定")
	case errors.Is(err, errGoogleEmailTaken):
		common.WriteError(w, http.StatusConflict, "AUTH_EMAIL_EXISTS", "该邮箱已绑定其他登录方式")
	case errors.Is(err, errGoogleNeedsLink):
		// No silent linking by email: a Google account that matches an existing
		// account must be linked from inside that account (see
		// /account/oauth/google/link), otherwise whoever controls the Google
		// account for that address could take over the exchange account.
		common.WriteError(w, http.StatusConflict, "GOOGLE_LINK_REQUIRED", "该邮箱已注册，请先登录账户后在安全中心绑定 Google，或使用邮箱密码登录")
	default:
		log.Printf("google user: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
}

var (
	errGoogleEmailUnverified = errors.New("email unverified locally")
	errGoogleEmailTaken      = errors.New("email taken")
	errGoogleNeedsLink       = errors.New("existing account must link google explicitly")
)

func (s *Server) googleKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	if s.googleKeysCache == nil {
		s.googleKeysCache = &googleJWKCache{}
	}
	s.googleKeysCache.mu.Lock()
	defer s.googleKeysCache.mu.Unlock()
	if len(s.googleKeysCache.keys) > 0 && time.Since(s.googleKeysCache.at) < time.Hour {
		return s.googleKeysCache.keys, nil
	}
	keys, err := fetchGoogleKeys(ctx, s.cfg.GoogleJWKSURL)
	if err != nil {
		if len(s.googleKeysCache.keys) > 0 {
			return s.googleKeysCache.keys, nil
		}
		return nil, err
	}
	s.googleKeysCache.keys = keys
	s.googleKeysCache.at = time.Now()
	return keys, nil
}

func fetchGoogleKeys(ctx context.Context, rawURL string) (map[string]*rsa.PublicKey, error) {
	if strings.TrimSpace(rawURL) == "" {
		rawURL = "https://www.googleapis.com/oauth2/v3/certs"
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("jwks status")
	}
	var body struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return googleKeysFromJWK(body.Keys)
}

func googleKeysFromJWK(keys []struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}) (map[string]*rsa.PublicKey, error) {
	out := map[string]*rsa.PublicKey{}
	for _, k := range keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
		eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(eb) == 0 {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		if e <= 0 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	if len(out) == 0 {
		return nil, errors.New("no rsa keys")
	}
	return out, nil
}

func verifyGoogleIDToken(raw string, keys map[string]*rsa.PublicKey, audiences []string, now time.Time) (googleIdentity, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}), jwt.WithLeeway(2*time.Minute), jwt.WithTimeFunc(func() time.Time { return now }))
	tok, err := parser.Parse(raw, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		k := keys[kid]
		if k == nil {
			return nil, errors.New("unknown kid")
		}
		return k, nil
	})
	if err != nil || !tok.Valid {
		return googleIdentity{}, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return googleIdentity{}, errors.New("claims")
	}
	iss, _ := claims["iss"].(string)
	if iss != "https://accounts.google.com" && iss != "accounts.google.com" {
		return googleIdentity{}, errors.New("iss")
	}
	if !audienceOK(claims["aud"], audiences) {
		return googleIdentity{}, errors.New("aud")
	}
	sub, _ := claims["sub"].(string)
	sub = strings.TrimSpace(sub)
	if sub == "" || len(sub) > 255 {
		return googleIdentity{}, errors.New("sub")
	}
	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	verified := claimBool(claims["email_verified"])
	if email == "" || !verified || !strings.Contains(email, "@") {
		return googleIdentity{}, errors.New("email")
	}
	return googleIdentity{Sub: sub, Email: email, EmailVerified: true}, nil
}

func audienceOK(aud any, want []string) bool {
	switch v := aud.(type) {
	case string:
		return inList(v, want)
	case []any:
		for _, x := range v {
			s, _ := x.(string)
			if inList(s, want) {
				return true
			}
		}
	}
	return false
}

func inList(s string, want []string) bool {
	for _, w := range want {
		if s != "" && s == w {
			return true
		}
	}
	return false
}

func claimBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	}
	return false
}

// handleGoogleLink binds a Google identity to the signed-in account. It
// replaces implicit linking by email: the owner proves control of the account
// (password + second factor) and the Google account must carry the same
// verified address.
//
//	POST /account/oauth/google/link  {idToken, totpCode?|code?}
func (s *Server) handleGoogleLink(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if len(s.cfg.GoogleClientIDs) == 0 {
		common.WriteError(w, http.StatusServiceUnavailable, "GOOGLE_UNCONFIGURED", "Google 登录未配置")
		return
	}
	var req struct {
		IDToken  string `json:"idToken"`
		TOTPCode string `json:"totpCode"`
		Code     string `json:"code"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.IDToken) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "idToken 必填")
		return
	}
	if pid, err := s.store.ParentIDOf(r.Context(), u.ID); err != nil || pid != 0 {
		common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_FORBIDDEN", "子账户不能绑定第三方登录")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	keys, err := s.googleKeys(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusServiceUnavailable, "GOOGLE_UNAVAILABLE", "暂时无法校验 Google 登录")
		return
	}
	id, err := verifyGoogleIDToken(req.IDToken, keys, s.cfg.GoogleClientIDs, s.now())
	if err != nil || !id.EmailVerified {
		common.WriteError(w, http.StatusUnauthorized, "GOOGLE_TOKEN_INVALID", "Google 登录凭证无效")
		return
	}
	// The Google address must be one this account already controls — otherwise
	// linking would let a different mail address act as a login for this account.
	if !u.Email.Valid || !strings.EqualFold(u.Email.String, id.Email) {
		common.WriteError(w, http.StatusConflict, "GOOGLE_EMAIL_MISMATCH", "Google 账号邮箱与本站账户邮箱不一致")
		return
	}
	owner, err := s.store.OAuthOwner(r.Context(), "google", id.Sub)
	if err != nil && !errors.Is(err, ErrNotFound) {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if owner != 0 && owner != u.ID {
		common.WriteError(w, http.StatusConflict, "GOOGLE_ALREADY_LINKED", "该 Google 账号已绑定其他账户")
		return
	}
	if owner == 0 {
		if err := s.store.LinkOAuthIdentity(r.Context(), "google", id.Sub, u.ID, id.Email); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	s.securityChanged(r.Context(), u, "绑定了 Google 登录", true)
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": "google"})
}

// OAuthOwner returns the local user id linked to (provider, subject), or
// ErrNotFound.
func (s *Store) OAuthOwner(ctx context.Context, provider, subject string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM oauth_identities WHERE provider=$1 AND subject=$2`, provider, subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return userID, err
}

// LinkOAuthIdentity binds an external identity to an existing account. The
// (provider, subject) pair is unique, so a concurrent link fails loudly.
func (s *Store) LinkOAuthIdentity(ctx context.Context, provider, subject string, userID int64, email string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_identities (provider, subject, user_id, email) VALUES ($1,$2,$3,$4)`,
		provider, subject, userID, email)
	return err
}

func (s *Server) googleUser(ctx context.Context, id googleIdentity) (*User, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM oauth_identities WHERE provider='google' AND subject=$1`, id.Sub).Scan(&userID)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.store.UserByID(ctx, userID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var existing int64
	var verified bool
	err = tx.QueryRowContext(ctx, `SELECT id, email_verified FROM users WHERE email=$1`, id.Email).Scan(&existing, &verified)
	switch {
	case err == nil && !verified:
		return nil, errGoogleEmailUnverified
	case err == nil:
		// An existing account is never linked implicitly: the owner must sign in
		// and link Google with their second factor (linkGoogleAccount).
		return nil, errGoogleNeedsLink
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	hash, err := hashPassword(base64.RawURLEncoding.EncodeToString(buf))
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, password_hash, email_verified) VALUES ($1,$2,TRUE) RETURNING id`, id.Email, hash).Scan(&userID)
	if err != nil {
		if constraint := uniqueConstraint(err); strings.Contains(constraint, "email") {
			return nil, errGoogleEmailTaken
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_identities (provider, subject, user_id, email) VALUES ('google',$1,$2,$3)`, id.Sub, userID, id.Email); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	u, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.initAssetAccount(ctx, u.UID)
	return u, nil
}
