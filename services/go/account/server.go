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
	recorder *CodeRecorder // non-nil only in dev mode (backs the dev last-code endpoint)
	http     *http.Client
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
	codes := NewCodeService(store, logEmailSender{rec: rec}, logSmsSender{rec: rec}, rec, rdb, now)
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
		recorder: rec,
		http:     &http.Client{Timeout: 5 * time.Second},
		now:      now,
	}
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
	mux.HandleFunc("POST /auth/password/reset", s.handlePasswordReset)

	// Authenticated endpoints.
	mux.Handle("POST /auth/logout", s.auth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /account", s.auth(http.HandlerFunc(s.handleAccount)))
	mux.Handle("PATCH /account/profile", s.auth(http.HandlerFunc(s.handleUpdateProfile)))
	mux.Handle("POST /account/code/send", s.auth(http.HandlerFunc(s.handleSendCodeAuthed)))
	mux.Handle("GET /account/devices", s.auth(http.HandlerFunc(s.handleListDevices)))
	mux.Handle("DELETE /account/devices/{deviceId}", s.auth(http.HandlerFunc(s.handleRemoveDevice)))
	mux.Handle("GET /account/withdraw-whitelist", s.auth(http.HandlerFunc(s.handleGetWhitelist)))
	mux.Handle("PUT /account/withdraw-whitelist", s.auth(http.HandlerFunc(s.handleSetWhitelist)))
	mux.Handle("GET /account/withdraw-addresses", s.auth(http.HandlerFunc(s.handleListAddresses)))
	mux.Handle("POST /account/withdraw-addresses", s.auth(http.HandlerFunc(s.handleCreateAddress)))
	mux.Handle("DELETE /account/withdraw-addresses/{id}", s.auth(http.HandlerFunc(s.handleDeleteAddress)))

	// 2FA (TOTP).
	mux.Handle("POST /2fa/totp/bind", s.auth(http.HandlerFunc(s.handleTOTPBind)))
	mux.Handle("POST /2fa/totp/verify", s.auth(http.HandlerFunc(s.handleTOTPVerify)))
	mux.Handle("POST /2fa/totp/disable", s.auth(http.HandlerFunc(s.handleTOTPDisable)))

	// Dev-only internal endpoint: read the most recent verification code so tests
	// and smoke scripts can complete flows without a real email/SMS provider.
	// Guarded by DEV_MODE + the shared internal token; never proxied by the gateway.
	if s.cfg.DevMode {
		mux.HandleFunc("GET /internal/dev/last-code", s.handleDevLastCode)
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

// deviceFrom builds a Device fingerprint from the request. A client-supplied
// deviceId is preferred; otherwise the User-Agent is hashed. The key is stable
// so the same browser/device is recognised across logins.
func deviceFrom(r *http.Request, deviceID, deviceName string) Device {
	ua := r.Header.Get("User-Agent")
	key := "ua:" + hashToken(ua)
	if deviceID = strings.TrimSpace(deviceID); deviceID != "" {
		key = "cid:" + deviceID
	}
	return Device{DeviceKey: key, Name: strings.TrimSpace(deviceName), UserAgent: ua, IP: clientIP(r)}
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
