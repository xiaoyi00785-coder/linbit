package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// ctxKey is the context key type for request-scoped values.
type ctxKey string

const ctxUID ctxKey = "uid"

// Server wires configuration, storage and HTTP handlers for the account service.
type Server struct {
	cfg   Config
	store *Store
	http  *http.Client
	now   func() time.Time
}

// NewServer constructs a Server.
func NewServer(cfg Config, store *Store) *Server {
	return &Server{
		cfg:   cfg,
		store: store,
		http:  &http.Client{Timeout: 5 * time.Second},
		now:   time.Now,
	}
}

// Routes returns the account service HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Public auth endpoints.
	mux.HandleFunc("POST /auth/register", s.handleRegister)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/refresh", s.handleRefresh)

	// Authenticated endpoints.
	mux.Handle("POST /auth/logout", s.auth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /account", s.auth(http.HandlerFunc(s.handleAccount)))
	mux.Handle("POST /2fa/totp/bind", s.auth(http.HandlerFunc(s.handleTOTPBind)))
	mux.Handle("POST /2fa/totp/verify", s.auth(http.HandlerFunc(s.handleTOTPVerify)))
	mux.Handle("POST /2fa/totp/disable", s.auth(http.HandlerFunc(s.handleTOTPDisable)))
	return mux
}

// auth validates the Bearer access token and injects the uid into context.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		claims, err := common.ParseAccessToken(s.cfg.JWTSecret, token)
		if err != nil {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "令牌无效或已过期")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUID, claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

// ---- DTOs ----

type registerRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	InviteCode string `json:"inviteCode"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type codeRequest struct {
	Code string `json:"code"`
}

type authTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int    `json:"expiresIn"`
}

type accountResponse struct {
	UID              string    `json:"uid"`
	Email            string    `json:"email"`
	Nickname         *string   `json:"nickname"`
	KYCLevel         int       `json:"kycLevel"`
	TwoFactorEnabled bool      `json:"twoFactorEnabled"`
	CreatedAt        time.Time `json:"createdAt"`
}

type twoFactorStatus struct {
	Enabled bool    `json:"enabled"`
	Method  *string `json:"method"`
}

// ---- Handlers ----

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "邮箱或密码不合法（密码至少 8 位）")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	u, err := s.store.CreateUser(r.Context(), req.Email, hash)
	if errors.Is(err, ErrEmailExists) {
		common.WriteError(w, http.StatusConflict, "AUTH_EMAIL_EXISTS", "该邮箱已被注册")
		return
	}
	if err != nil {
		log.Printf("register: create user: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	// Initialise the user's asset account (best-effort; balances are also
	// lazily seeded by the asset service on first read).
	s.initAssetAccount(r.Context(), u.UID)

	tokens, err := s.issueTokens(r.Context(), u)
	if err != nil {
		log.Printf("register: issue tokens: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, tokens)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	u, err := s.store.UserByEmail(r.Context(), req.Email)
	if err != nil || !checkPassword(u.PasswordHash, req.Password) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "邮箱或密码错误")
		return
	}
	if u.TOTPEnabled {
		if req.TOTPCode == "" || !validateTOTP(u.TOTPSecret, req.TOTPCode) {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_TOTP_REQUIRED", "需要有效的两步验证码")
			return
		}
	}
	tokens, err := s.issueTokens(r.Context(), u)
	if err != nil {
		log.Printf("login: issue tokens: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.RefreshToken == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	u, err := s.store.ConsumeRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "刷新令牌无效或已过期")
		return
	}
	tokens, err := s.issueTokens(r.Context(), u)
	if err != nil {
		log.Printf("refresh: issue tokens: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	uid := r.Context().Value(ctxUID).(string)
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if err := s.store.RevokeAllRefreshTokens(r.Context(), u.ID); err != nil {
		log.Printf("logout: revoke: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	uid := r.Context().Value(ctxUID).(string)
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var nickname *string
	if u.Nickname.Valid {
		nickname = &u.Nickname.String
	}
	common.WriteJSON(w, http.StatusOK, accountResponse{
		UID:              u.UID,
		Email:            u.Email,
		Nickname:         nickname,
		KYCLevel:         u.KYCLevel,
		TwoFactorEnabled: u.TOTPEnabled,
		CreatedAt:        u.CreatedAt,
	})
}

func (s *Server) handleTOTPBind(w http.ResponseWriter, r *http.Request) {
	uid := r.Context().Value(ctxUID).(string)
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	secret, uri, err := generateTOTP(u.Email)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.store.SetTOTPSecret(r.Context(), uid, secret); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauthUri": uri})
}

func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	uid := r.Context().Value(ctxUID).(string)
	var req codeRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil || u.TOTPSecret == "" {
		common.WriteError(w, http.StatusBadRequest, "TOTP_NOT_BOUND", "请先绑定 TOTP")
		return
	}
	if !validateTOTP(u.TOTPSecret, req.Code) {
		common.WriteError(w, http.StatusBadRequest, "TOTP_INVALID", "验证码错误")
		return
	}
	if err := s.store.SetTOTPEnabled(r.Context(), uid, true); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	method := "totp"
	common.WriteJSON(w, http.StatusOK, twoFactorStatus{Enabled: true, Method: &method})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	uid := r.Context().Value(ctxUID).(string)
	var req codeRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.Code == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil || !u.TOTPEnabled {
		common.WriteError(w, http.StatusBadRequest, "TOTP_NOT_ENABLED", "两步验证未启用")
		return
	}
	if !validateTOTP(u.TOTPSecret, req.Code) {
		common.WriteError(w, http.StatusBadRequest, "TOTP_INVALID", "验证码错误")
		return
	}
	if err := s.store.SetTOTPEnabled(r.Context(), uid, false); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, twoFactorStatus{Enabled: false, Method: nil})
}

// issueTokens signs an access token and persists a rotated refresh token.
func (s *Server) issueTokens(ctx context.Context, u *User) (authTokens, error) {
	now := s.now()
	access, err := common.SignAccessToken(s.cfg.JWTSecret, u.UID, u.Email, s.cfg.AccessTTL, now)
	if err != nil {
		return authTokens{}, err
	}
	refresh, err := newRefreshToken()
	if err != nil {
		return authTokens{}, err
	}
	if err := s.store.StoreRefreshToken(ctx, u.ID, refresh, now.Add(s.cfg.RefreshTTL)); err != nil {
		return authTokens{}, err
	}
	return authTokens{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.cfg.AccessTTL.Seconds()),
	}, nil
}

// initAssetAccount asks the asset service to create the user's asset account.
func (s *Server) initAssetAccount(ctx context.Context, uid string) {
	if s.cfg.AssetInternalURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"userId": uid})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.AssetInternalURL, "/")+"/internal/accounts", bytes.NewReader(body))
	if err != nil {
		log.Printf("initAssetAccount: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := s.http.Do(req)
	if err != nil {
		log.Printf("initAssetAccount: call asset: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("initAssetAccount: asset returned %d", resp.StatusCode)
	}
}
