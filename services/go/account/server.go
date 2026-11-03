package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/common/storage"
	"github.com/redis/go-redis/v9"
)

// ctxKey is the context key type for request-scoped values.
type ctxKey string

const ctxUID ctxKey = "uid"

// Server wires configuration, storage and HTTP handlers for the account service.
type Server struct {
	cfg      Config
	store    *Store
	codes    *CodeService
	mail     EmailSender   // notification email channel
	recorder *CodeRecorder // non-nil only in dev mode (backs the dev last-code endpoint)
	http     *http.Client
	avatars  storage.ObjectStorage // pluggable blob store for avatar uploads
	now      func() time.Time
}

// NewServer constructs a Server. rdb may be nil (Redis is optional; the code
// rate-limiter then falls back to database counters).
func NewServer(cfg Config, store *Store, rdb *redis.Client) *Server {
	now := time.Now
	var rec *CodeRecorder
	if cfg.DevMode {
		rec = NewCodeRecorder()
	}
	mail := EmailSender(logEmailSender{rec: rec})
	codes := NewCodeService(store, mail, logSmsSender{rec: rec}, rec, rdb, now)
	if cfg.DevMode {
		// Dev/test: relax send throttling so smoke scripts can drive several
		// code-gated operations back-to-back. Production keeps the real limits.
		codes.minInterval = 0
		codes.hourlyMax = 1 << 30
	}
	return &Server{
		cfg:      cfg,
		store:    store,
		codes:    codes,
		mail:     mail,
		recorder: rec,
		http:     &http.Client{Timeout: 5 * time.Second},
		avatars:  newAvatarStorage(cfg),
		now:      now,
	}
}

// newAvatarStorage builds the configured ObjectStorage backend for avatars. The
// local backend stores on disk; "s3" selects the (skeleton) S3 backend. A local
// backend that fails to initialise (e.g. undeletable dir) leaves avatars nil,
// and the upload handler reports the feature as unavailable.
func newAvatarStorage(cfg Config) storage.ObjectStorage {
	if strings.EqualFold(cfg.StorageBackend, "s3") {
		return storage.NewS3Storage(storage.S3Config{
			Bucket:     cfg.S3Bucket,
			Region:     cfg.S3Region,
			Endpoint:   cfg.S3Endpoint,
			PublicBase: cfg.S3PublicBase,
		})
	}
	ls, err := storage.NewLocalStorage(cfg.AvatarDir, cfg.AvatarPublicBase)
	if err != nil {
		log.Printf("avatar storage: init local backend: %v (avatar upload disabled)", err)
		return nil
	}
	return ls
}

// Routes returns the account service HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Public auth endpoints.
	mux.HandleFunc("POST /auth/code/send", s.handleSendCode)
	mux.HandleFunc("POST /auth/register", s.handleRegister)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/login/verify", s.handleLoginVerify)
	mux.HandleFunc("POST /auth/login/code", s.handleLoginCode)
	mux.HandleFunc("POST /auth/refresh", s.handleRefresh)
	mux.HandleFunc("POST /auth/password/verify-code", s.handlePasswordVerifyCode)
	mux.HandleFunc("POST /auth/password/reset", s.handlePasswordReset)

	// Public avatar retrieval: a controlled route that streams stored avatars
	// (local backend only) from the configured directory, blocking traversal.
	mux.HandleFunc("GET /account/avatars/{rest...}", s.handleServeAvatar)

	// Authenticated endpoints.
	mux.Handle("POST /auth/logout", s.auth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /account", s.auth(http.HandlerFunc(s.handleAccount)))
	mux.Handle("PATCH /account/profile", s.auth(http.HandlerFunc(s.handleUpdateProfile)))
	mux.Handle("POST /account/avatar", s.auth(http.HandlerFunc(s.handleUploadAvatar)))
	mux.Handle("POST /account/code/send", s.auth(http.HandlerFunc(s.handleSendCodeAuthed)))
	mux.Handle("GET /account/devices", s.auth(http.HandlerFunc(s.handleListDevices)))
	mux.Handle("DELETE /account/devices/{deviceId}", s.auth(http.HandlerFunc(s.handleRemoveDevice)))
	mux.Handle("GET /account/withdraw-whitelist", s.auth(http.HandlerFunc(s.handleGetWhitelist)))
	mux.Handle("PUT /account/withdraw-whitelist", s.auth(http.HandlerFunc(s.handleSetWhitelist)))
	mux.Handle("GET /account/withdraw-addresses", s.auth(http.HandlerFunc(s.handleListAddresses)))
	mux.Handle("POST /account/withdraw-addresses", s.auth(http.HandlerFunc(s.handleCreateAddress)))
	mux.Handle("DELETE /account/withdraw-addresses/{id}", s.auth(http.HandlerFunc(s.handleDeleteAddress)))

	// Message center (in-app notifications + email channel preferences).
	mux.Handle("GET /notifications", s.auth(http.HandlerFunc(s.handleListNotifications)))
	mux.Handle("GET /notifications/unread-count", s.auth(http.HandlerFunc(s.handleUnreadCount)))
	mux.Handle("POST /notifications/read", s.auth(http.HandlerFunc(s.handleReadNotifications)))
	mux.Handle("GET /notifications/settings", s.auth(http.HandlerFunc(s.handleGetNotifyPrefs)))
	mux.Handle("PUT /notifications/settings", s.auth(http.HandlerFunc(s.handleSetNotifyPrefs)))
	mux.HandleFunc("POST /internal/notifications", s.handleInternalNotify)
	mux.HandleFunc("POST /internal/notifications/broadcast", s.handleInternalBroadcast)

	// 2FA (TOTP).
	mux.Handle("POST /2fa/totp/bind", s.auth(http.HandlerFunc(s.handleTOTPBind)))
	mux.Handle("POST /2fa/totp/verify", s.auth(http.HandlerFunc(s.handleTOTPVerify)))
	mux.Handle("POST /2fa/totp/disable", s.auth(http.HandlerFunc(s.handleTOTPDisable)))

	// Service-to-service endpoints (internal token; never proxied by the gateway).
	// Used by the wallet service to gate withdrawals: verify the second factor and
	// check the destination against the user's withdrawal whitelist.
	mux.HandleFunc("POST /internal/withdraw/verify", s.handleInternalWithdrawVerify)
	mux.HandleFunc("GET /internal/withdraw/whitelist", s.handleInternalWithdrawWhitelist)
	// Used by the KYC service to read (wallet limits) and raise (on approval) a
	// user's kycLevel.
	mux.HandleFunc("GET /internal/account/kyc-level", s.handleInternalGetKYCLevel)
	mux.HandleFunc("POST /internal/account/kyc-level", s.handleInternalSetKYCLevel)
	mux.HandleFunc("GET /internal/admin/users", s.handleInternalAdminUsers)
	mux.HandleFunc("GET /internal/users/feed", s.handleInternalUsersFeed)
	mux.HandleFunc("GET /internal/users/profile", s.handleInternalUserProfile)
	mux.HandleFunc("GET /internal/admin/users/{uid}", s.handleInternalAdminUser)
	mux.HandleFunc("POST /internal/admin/users/{uid}/status", s.handleInternalAdminSetStatus)

	// Dev-only internal endpoint: read the most recent verification code so tests
	// and smoke scripts can complete flows without a real email/SMS provider.
	// Guarded by DEV_MODE + the shared internal token; never proxied by the gateway.
	if s.cfg.DevMode {
		mux.HandleFunc("GET /internal/dev/last-code", s.handleDevLastCode)
		mux.HandleFunc("POST /internal/dev/account/bind-phone", s.handleDevBindPhone)
	}
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

// authedUser resolves the authenticated user from the request context.
func (s *Server) authedUser(r *http.Request) (*User, bool) {
	uid, _ := r.Context().Value(ctxUID).(string)
	if uid == "" {
		return nil, false
	}
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil {
		return nil, false
	}
	return u, true
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

// clientIP returns the best-effort client IP (honouring X-Forwarded-For set by
// the gateway), used only for device bookkeeping.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// deviceKeyFor computes the stable device fingerprint key for a request. An
// explicit client-supplied deviceId wins; otherwise it falls back to the
// X-Device-Id header or a deviceId query param (so non-login requests such as
// the device list can identify "this" device), and finally to a User-Agent hash.
// The key is stable so the same browser/device is recognised across requests.
func deviceKeyFor(r *http.Request, deviceID string) string {
	if deviceID = strings.TrimSpace(deviceID); deviceID != "" {
		return "cid:" + deviceID
	}
	if h := strings.TrimSpace(r.Header.Get("X-Device-Id")); h != "" {
		return "cid:" + h
	}
	if q := strings.TrimSpace(r.URL.Query().Get("deviceId")); q != "" {
		return "cid:" + q
	}
	return "ua:" + hashToken(r.Header.Get("User-Agent"))
}

// deviceFrom builds a Device fingerprint from the request (see deviceKeyFor for
// the key derivation).
func deviceFrom(r *http.Request, deviceID, deviceName string) Device {
	return Device{
		DeviceKey: deviceKeyFor(r, deviceID),
		Name:      strings.TrimSpace(deviceName),
		UserAgent: r.Header.Get("User-Agent"),
		IP:        clientIP(r),
	}
}

// ---- shared DTOs ----

type authTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int    `json:"expiresIn"`
}

type accountResponse struct {
	UID                      string    `json:"uid"`
	Email                    *string   `json:"email"`
	Phone                    *string   `json:"phone"`
	CountryCode              string    `json:"countryCode"`
	Nickname                 *string   `json:"nickname"`
	AvatarURL                *string   `json:"avatarUrl"`
	KYCLevel                 int       `json:"kycLevel"`
	TwoFactorEnabled         bool      `json:"twoFactorEnabled"`
	EmailVerified            bool      `json:"emailVerified"`
	PhoneVerified            bool      `json:"phoneVerified"`
	WithdrawWhitelistEnabled bool      `json:"withdrawWhitelistEnabled"`
	CreatedAt                time.Time `json:"createdAt"`
}

func toAccountResponse(u *User) accountResponse {
	resp := accountResponse{
		UID:                      u.UID,
		CountryCode:              u.CountryCode,
		KYCLevel:                 u.KYCLevel,
		TwoFactorEnabled:         u.TOTPEnabled,
		EmailVerified:            u.EmailVerified,
		PhoneVerified:            u.PhoneVerified,
		WithdrawWhitelistEnabled: u.WithdrawWhitelistEnabled,
		CreatedAt:                u.CreatedAt,
	}
	if u.Email.Valid {
		resp.Email = &u.Email.String
	}
	if u.Phone.Valid {
		resp.Phone = &u.Phone.String
	}
	if u.Nickname.Valid {
		resp.Nickname = &u.Nickname.String
	}
	if u.AvatarURL != "" {
		resp.AvatarURL = &u.AvatarURL
	}
	return resp
}

// ---- account handler ----

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	common.WriteJSON(w, http.StatusOK, toAccountResponse(u))
}

// handleDevLastCode returns the most recent verification code for a target
// (dev mode only, internal-token protected).
func (s *Server) handleDevLastCode(w http.ResponseWriter, r *http.Request) {
	if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "internal token required")
		return
	}
	target := r.URL.Query().Get("target")
	purpose := r.URL.Query().Get("purpose")
	code := s.recorder.Last(target, purpose)
	if code == "" {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no code for target/purpose")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"target": target, "purpose": purpose, "code": code})
}

// issueTokens signs an access token and persists a rotated refresh token bound
// to the given device.
func (s *Server) issueTokens(ctx context.Context, u *User, deviceKey string) (authTokens, error) {
	if u.Status == userFrozen {
		return authTokens{}, ErrAccountFrozen
	}
	now := s.now()
	subEmail := ""
	if u.Email.Valid {
		subEmail = u.Email.String
	}
	access, err := common.SignAccessToken(s.cfg.JWTSecret, u.UID, subEmail, s.cfg.AccessTTL, now)
	if err != nil {
		return authTokens{}, err
	}
	refresh, err := newRefreshToken()
	if err != nil {
		return authTokens{}, err
	}
	if err := s.store.StoreRefreshToken(ctx, u.ID, refresh, deviceKey, now.Add(s.cfg.RefreshTTL)); err != nil {
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

// handleDevBindPhone (dev only) attaches a verified phone to a user so smoke
// scripts can exercise the "bind email + phone" newcomer task.
func (s *Server) handleDevBindPhone(w http.ResponseWriter, r *http.Request) {
	if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "internal token required")
		return
	}
	var req struct {
		UID   string `json:"uid"`
		Phone string `json:"phone"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.UID == "" || req.Phone == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if _, err := s.store.db.ExecContext(r.Context(), `UPDATE users SET phone=$2, phone_verified=TRUE, email_verified=(email IS NOT NULL) WHERE uid=$1`, req.UID, req.Phone); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
