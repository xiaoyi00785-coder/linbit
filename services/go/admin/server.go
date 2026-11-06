package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// Server is the admin HTTP API.
type Server struct {
	cfg  Config
	db   *sql.DB
	http *http.Client
	now  func() time.Time
}

// NewServer builds the server.
func NewServer(cfg Config, db *sql.DB) *Server {
	return &Server{cfg: cfg, db: db, http: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

// actor is the authenticated admin of a request.
type actor struct {
	ID       int64
	Username string
	Roles    []string
	TOTP     string
	reqID    string
}

type ctxKey struct{}

func actorOf(r *http.Request) *actor { a, _ := r.Context().Value(ctxKey{}).(*actor); return a }

// Routes builds the mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// Public (no admin auth): published announcements.
	mux.HandleFunc("GET /announcements", s.handlePublicAnnouncements)
	mux.HandleFunc("GET /announcements/{id}", s.handlePublicAnnouncement)
	mux.HandleFunc("GET /site/social", s.handlePublicSocial)

	a := func(pattern, perm string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.guard(s.authed(perm, h)))
	}
	mux.HandleFunc("POST /admin/v1/auth/login", s.guard(s.handleLogin))
	a("POST /admin/v1/auth/logout", "", s.handleLogout)
	a("GET /admin/v1/me", "", s.handleMe)

	a("GET /admin/v1/admins", permAdminManage, s.handleListAdmins)
	a("POST /admin/v1/admins", permAdminManage, s.handleCreateAdmin)
	a("PATCH /admin/v1/admins/{id}", permAdminManage, s.handlePatchAdmin)

	a("GET /admin/v1/users", permUserView, s.handleListUsers)
	a("GET /admin/v1/users/{uid}", permUserView, s.handleGetUser)
	a("POST /admin/v1/users/{uid}/freeze", permUserEdit, s.handleFreeze(true))
	a("POST /admin/v1/users/{uid}/unfreeze", permUserEdit, s.handleFreeze(false))
	a("GET /admin/v1/sub-accounts", permUserView, s.handleListSubAccounts)
	a("GET /admin/v1/sub-accounts/settings", permUserView, s.handleGetSubSettings)
	a("PUT /admin/v1/sub-accounts/settings", permUserEdit, s.handlePutSubSettings)
	a("POST /admin/v1/sub-accounts/{uid}/freeze", permUserEdit, s.handleFreeze(true))
	a("POST /admin/v1/sub-accounts/{uid}/unfreeze", permUserEdit, s.handleFreeze(false))
	a("GET /admin/v1/users/{uid}/balances", permLedgerView, s.handleUserBalances)

	a("GET /admin/v1/kyc/applications", permKYCView, s.handleKYCList)
	a("GET /admin/v1/kyc/applications/{id}", permKYCView, s.handleKYCGet)
	a("POST /admin/v1/kyc/applications/{id}/approve", permKYCApprove, s.handleKYCDecision("approve"))
	a("POST /admin/v1/kyc/applications/{id}/reject", permKYCApprove, s.handleKYCDecision("reject"))
	a("GET /admin/v1/kyc/documents/{id}", permKYCView, s.handleKYCDocument)

	a("GET /admin/v1/withdrawals", permWithdrawView, s.handleWithdrawals)
	a("POST /admin/v1/withdrawals/{id}/approve", permWithdrawView, s.handleWithdrawApprove)
	a("POST /admin/v1/withdrawals/{id}/reject", permWithdrawView, s.handleWithdrawReject)

	a("GET /admin/v1/ledger", permLedgerView, s.handleLedger)

	a("GET /admin/v1/symbols", permSymbolView, s.handleSymbols)
	a("POST /admin/v1/symbols", permSymbolEdit, s.handleCreateSymbol)
	a("PATCH /admin/v1/symbols/{symbol}", permSymbolEdit, s.handlePatchSymbol)

	a("GET /admin/v1/site/social", permContentEdit, s.handleSocialGet)
	a("PUT /admin/v1/site/social", permContentEdit, s.handleSocialPut)

	s.assetGapRoutes(a)
	s.viewRoutes(a)
	a("GET /admin/v1/fees", permFeeView, s.handleGetFees)
	a("GET /admin/v1/fee-changes", permFeeView, s.handleListFeeChanges)
	a("POST /admin/v1/fee-changes", permFeeEdit, s.handleSubmitFeeChange)
	a("POST /admin/v1/fee-changes/{id}/approve", permFeeView, s.handleFeeDecision("approve"))
	a("POST /admin/v1/fee-changes/{id}/reject", permFeeView, s.handleFeeDecision("reject"))
	a("POST /admin/v1/fee-changes/{id}/rollback", permFeeEdit, s.handleFeeRollback)

	a("GET /admin/v1/coupons", permRewardView, s.handleListCoupons)
	a("POST /admin/v1/coupons", permRewardEdit, s.handleCreateCoupon)
	a("POST /admin/v1/coupons/{id}/approve", permCouponApprove, s.handleCouponDecision("approve"))
	a("POST /admin/v1/coupons/{id}/reject", permCouponApprove, s.handleCouponDecision("reject"))

	a("GET /admin/v1/rewards", permRewardView, s.handleListRewards)
	a("POST /admin/v1/rewards", permRewardEdit, s.handleCreateReward)
	a("POST /admin/v1/rewards/{id}/approve", permRewardApprove, s.handleRewardDecision("approve"))
	a("POST /admin/v1/rewards/{id}/reject", permRewardApprove, s.handleRewardDecision("reject"))

	a("GET /admin/v1/announcements", permContentEdit, s.handleListAnnouncements)
	a("POST /admin/v1/announcements", permContentEdit, s.handleCreateAnnouncement)
	a("PUT /admin/v1/announcements/{id}", permContentEdit, s.handleUpdateAnnouncement)
	a("DELETE /admin/v1/announcements/{id}", permContentEdit, s.handleDeleteAnnouncement)

	a("GET /admin/v1/copy/applications", permCopyView, s.handleCopyApplications)
	a("POST /admin/v1/copy/applications/{uid}/approve", permCopyReview, s.handleCopyReview("approve"))
	a("POST /admin/v1/copy/applications/{uid}/reject", permCopyReview, s.handleCopyReview("reject"))
	a("GET /admin/v1/referrals/{uid}", permUserView, s.handleReferralLookup)
	a("GET /admin/v1/partners", permPartnerView, s.handlePartnerApplications)
	a("POST /admin/v1/partners/{uid}/approve", permPartnerReview, s.handlePartnerReview("approve"))
	a("POST /admin/v1/partners/{uid}/reject", permPartnerReview, s.handlePartnerReview("reject"))
	a("POST /admin/v1/partners/{uid}/disable", permPartnerReview, s.handlePartnerReview("disable"))
	a("GET /admin/v1/points/campaigns", permRewardView, s.handlePointsCampaigns)
	a("POST /admin/v1/points/campaigns", permRewardEdit, s.handlePointsCampaignCreate)
	a("GET /admin/v1/users/{uid}/coupons", permUserView, s.handleUserCoupons)

	a("POST /admin/v1/notifications/broadcast", permContentEdit, s.handleNotifyBroadcast)
	a("POST /admin/v1/users/{uid}/notifications", permContentEdit, s.handleNotifyUser)

	a("GET /admin/v1/vip/applications", permVIPView, s.handleVIPApplications)
	a("POST /admin/v1/vip/applications/{id}/approve", permVIPReview, s.handleVIPReview("approve"))
	a("POST /admin/v1/vip/applications/{id}/reject", permVIPReview, s.handleVIPReview("reject"))
	a("GET /admin/v1/support/tickets", permSupportView, s.handleSupportTickets)
	a("GET /admin/v1/support/tickets/{id}", permSupportView, s.handleSupportTicket)
	a("GET /admin/v1/support/attachments/{id}", permSupportView, s.handleSupportAttachment)
	a("GET /admin/v1/support/assignees", permSupportView, s.handleSupportAssignees)
	a("POST /admin/v1/support/tickets/{id}/messages", permSupportReply, s.handleSupportReply)
	a("PATCH /admin/v1/support/tickets/{id}", permSupportReply, s.handleSupportPatch)
	// market maker (writes need mm:edit + 2FA, all audited)
	a("GET /admin/v1/mm/status", permMMView, s.handleMMGet("/internal/mm/status"))
	a("GET /admin/v1/mm/pairs", permMMView, s.handleMMGet("/internal/mm/pairs"))
	a("PUT /admin/v1/mm/pairs/{market}/{symbol}", permMMEdit, s.handleMMUpdatePair)
	a("POST /admin/v1/mm/start", permMMEdit, s.handleMMRun("start"))
	a("POST /admin/v1/mm/stop", permMMEdit, s.handleMMRun("stop"))
	a("POST /admin/v1/mm/testnet-seed", permMMEdit, s.handleMMSeed)
	a("POST /admin/v1/mm/futures-transfer", permMMEdit, s.handleMMTransfer)
	a("GET /admin/v1/mm/baselines", permMMView, s.handleMMGet("/internal/mm/baselines"))
	a("PUT /admin/v1/mm/baselines/{asset}", permMMEdit, s.handleMMUpdateBaseline)
	// daily solvency reconciliation
	a("GET /admin/v1/solvency/latest", permMMView, s.handleSolvencyLatest)
	a("POST /admin/v1/solvency/run", permMMEdit, s.handleSolvencyRun)

	s.registerEarnRoutes(a)
	// 闪兑 convert: config reads need symbol:view; writes symbol:edit + 2FA (audited)
	a("GET /admin/v1/convert/config", permSymbolView, s.handleAssetGet("/internal/convert/config", nil))
	a("PUT /admin/v1/convert/config", permSymbolEdit, s.handleConvertConfig)
	a("GET /admin/v1/convert/orders", permLedgerView, s.handleAssetGet("/internal/convert/orders", []string{"userId", "asset", "page", "pageSize"}))
	a("GET /admin/v1/convert/position", permLedgerView, s.handleAssetGet("/internal/convert/position", nil))
	a("GET /admin/v1/convert/hedges", permLedgerView, s.handleAssetGet("/internal/convert/hedges", []string{"asset", "status", "page", "pageSize"}))
	a("GET /admin/v1/convert/events", permLedgerView, s.handleAssetGet("/internal/convert/events", []string{"kind", "page", "pageSize"}))
	s.registerHomeRoutes(a)
	s.registerChatRoutes(a)
	s.registerPorRoutes(a)
	s.registerUserCreditRoutes(a)
	s.registerAdjustRoutes(a)
	s.registerEventRoutes(a)
	s.registerTradFiRoutes(a)

	a("GET /admin/v1/audit-logs", permAuditView, s.handleAuditLogs)

	if s.cfg.DevMode {
		// Dev only: run the scheduler once at an arbitrary "now" so the smoke can
		// exercise the 24h fee notice without waiting.
		a("POST /admin/v1/dev/run-scheduler", permFeeEdit, s.handleDevRunScheduler)
	}
	return mux
}

// clientIP returns the caller IP (X-Forwarded-For only when trusted).
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) ipAllowed(ip string) bool {
	if len(s.cfg.IPAllowlist) == 0 {
		return true
	}
	pip := net.ParseIP(ip)
	for _, e := range s.cfg.IPAllowlist {
		if strings.Contains(e, "/") {
			if _, n, err := net.ParseCIDR(e); err == nil && pip != nil && n.Contains(pip) {
				return true
			}
		} else if e == ip {
			return true
		}
	}
	return false
}

// guard enforces the IP allowlist for every /admin/v1 route.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.ipAllowed(s.clientIP(r)) {
			s.audit(r, nil, "auth", "ip_denied", r.URL.Path, levelSensitive, "denied", nil, nil, "")
			common.WriteError(w, http.StatusForbidden, "IP_NOT_ALLOWED", "来源 IP 不在白名单")
			return
		}
		next(w, r)
	}
}

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

// authed resolves the bearer session, applies the idle timeout and checks perm.
func (s *Server) authed(perm string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" || tok == r.Header.Get("Authorization") {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "未登录")
			return
		}
		th := hashToken(tok)
		a := &actor{reqID: common.RequestID()}
		var lastSeen time.Time
		var revoked bool
		var status string
		var secret sql.NullString
		var roles []string
		err := s.db.QueryRowContext(r.Context(), `
			SELECT u.id, u.username, u.roles, u.status, u.totp_secret, ss.last_seen_at, ss.revoked
			FROM admin_sessions ss JOIN admin_users u ON u.id = ss.admin_id
			WHERE ss.token_hash = $1`, th).Scan(&a.ID, &a.Username, (*textArray)(&roles), &status, &secret, &lastSeen, &revoked)
		if err != nil || revoked || status != "active" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "会话无效")
			return
		}
		now := s.now()
		if now.Sub(lastSeen) > s.cfg.SessionIdle {
			_, _ = s.db.ExecContext(r.Context(), `UPDATE admin_sessions SET revoked = true WHERE token_hash = $1`, th)
			common.WriteError(w, http.StatusUnauthorized, "SESSION_EXPIRED", "会话已超时，请重新登录")
			return
		}
		_, _ = s.db.ExecContext(r.Context(), `UPDATE admin_sessions SET last_seen_at = $2 WHERE token_hash = $1`, th, now)
		a.Roles, a.TOTP = roles, secret.String
		if perm != "" && !can(a.Roles, perm) {
			s.audit(r, a, "auth", "forbidden", r.Method+" "+r.URL.Path, levelSensitive, "denied", nil, nil, perm)
			common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "无此权限")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, a)))
	}
}

// verify2FA re-checks the operator's TOTP for a sensitive action.
func (s *Server) verify2FA(w http.ResponseWriter, a *actor, code string) bool {
	if a.TOTP == "" {
		if s.cfg.Require2FA {
			common.WriteError(w, http.StatusForbidden, "TWO_FACTOR_REQUIRED", "账号未绑定 2FA")
			return false
		}
		return true
	}
	if !totp.Validate(strings.TrimSpace(code), a.TOTP) {
		common.WriteError(w, http.StatusUnauthorized, "TOTP_INVALID", "2FA 验证码错误")
		return false
	}
	return true
}

// ---- audit ----

const (
	levelNormal    = "normal"
	levelSensitive = "sensitive"
	levelHigh      = "high"
)

// audit appends an audit record. Failures are logged, never swallowed silently.
func (s *Server) audit(r *http.Request, a *actor, module, action, target, level, result string, before, after interface{}, reason string) {
	var id sql.NullInt64
	var name sql.NullString
	roles := []string{}
	reqID := ""
	if a != nil {
		id = sql.NullInt64{Int64: a.ID, Valid: true}
		name = sql.NullString{String: a.Username, Valid: true}
		roles, reqID = a.Roles, a.reqID
	}
	if reqID == "" {
		reqID = common.RequestID()
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO audit_logs (admin_id, admin_name, roles, module, action, target, ip, user_agent, request_id, level, result, before, after, reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		id, name, textArray(roles), module, action, target, s.clientIP(r), r.UserAgent(), reqID, level, result,
		jsonOrNull(before), jsonOrNull(after), nullStr(reason))
	if err != nil {
		log.Printf("audit insert failed (%s/%s): %v", module, action, err)
	}
}

func jsonOrNull(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		if len(raw) == 0 {
			return nil
		}
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ---- auth handlers ----

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := common.DecodeJSON(r, &req); err != nil || req.Username == "" || req.Password == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少用户名或密码")
		return
	}
	var (
		id     int64
		hash   string
		secret sql.NullString
		status string
		failed int
		locked sql.NullTime
		roles  []string
	)
	err := s.db.QueryRowContext(r.Context(), `SELECT id, password_hash, totp_secret, status, failed_attempts, locked_until, roles
		FROM admin_users WHERE username = $1`, req.Username).Scan(&id, &hash, &secret, &status, &failed, &locked, (*textArray)(&roles))
	a := &actor{ID: id, Username: req.Username, Roles: roles}
	if errors.Is(err, sql.ErrNoRows) {
		s.audit(r, nil, "auth", "login", req.Username, levelSensitive, "failed", nil, nil, "unknown user")
		common.WriteError(w, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
	if err != nil {
		internalErr(w, "login", err)
		return
	}
	now := s.now()
	if status != "active" {
		s.audit(r, a, "auth", "login", req.Username, levelSensitive, "failed", nil, nil, "disabled")
		common.WriteError(w, http.StatusForbidden, "ADMIN_DISABLED", "账号已停用")
		return
	}
	if locked.Valid && locked.Time.After(now) {
		s.audit(r, a, "auth", "login", req.Username, levelSensitive, "failed", nil, nil, "locked")
		common.WriteError(w, http.StatusLocked, "ADMIN_LOCKED", "登录失败次数过多，账号已临时锁定")
		return
	}
	okPass := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) == nil
	okTOTP := true
	if secret.Valid && secret.String != "" {
		okTOTP = totp.Validate(strings.TrimSpace(req.TOTPCode), secret.String)
	} else if s.cfg.Require2FA {
		okTOTP = false
	}
	if !okPass || !okTOTP {
		failed++
		var lock interface{}
		if failed >= s.cfg.MaxFailedLogins {
			lock, failed = now.Add(s.cfg.LockoutDuration), 0
		}
		_, _ = s.db.ExecContext(r.Context(), `UPDATE admin_users SET failed_attempts = $2, locked_until = COALESCE($3, locked_until) WHERE id = $1`, id, failed, lock)
		reason := "bad password"
		if okPass {
			reason = "bad totp"
		}
		s.audit(r, a, "auth", "login", req.Username, levelSensitive, "failed", nil, nil, reason)
		if okPass {
			common.WriteError(w, http.StatusUnauthorized, "TOTP_INVALID", "2FA 验证码错误")
		} else {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "用户名或密码错误")
		}
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		internalErr(w, "login rand", err)
		return
	}
	tok := hex.EncodeToString(buf)
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO admin_sessions (token_hash, admin_id, ip, user_agent, created_at, last_seen_at) VALUES ($1,$2,$3,$4,$5,$5)`,
		hashToken(tok), id, s.clientIP(r), r.UserAgent(), now); err != nil {
		internalErr(w, "login session", err)
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `UPDATE admin_users SET failed_attempts = 0, locked_until = NULL WHERE id = $1`, id)
	s.audit(r, a, "auth", "login", req.Username, levelSensitive, "ok", nil, nil, "")
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"token": tok, "admin": map[string]interface{}{"id": id, "username": req.Username, "roles": roles},
		"idleTimeoutSeconds": int(s.cfg.SessionIdle.Seconds()),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	_, _ = s.db.ExecContext(r.Context(), `UPDATE admin_sessions SET revoked = true WHERE token_hash = $1`, hashToken(tok))
	s.audit(r, actorOf(r), "auth", "logout", "", levelNormal, "ok", nil, nil, "")
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	perms := []string{}
	for _, p := range allPerms {
		if can(a.Roles, p) {
			perms = append(perms, p)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id": a.ID, "username": a.Username, "roles": a.Roles, "permissions": perms, "totpEnabled": a.TOTP != "",
	})
}

// ---- helpers ----

func internalErr(w http.ResponseWriter, where string, err error) {
	log.Printf("%s: %v", where, err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func upstreamErr(w http.ResponseWriter, where string, err error) {
	log.Printf("%s upstream: %v", where, err)
	common.WriteError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "下游服务不可用")
}

func pageParams(r *http.Request) (page, size int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	size, _ = strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	return
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
