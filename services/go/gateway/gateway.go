package main

import (
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/redis/go-redis/v9"
)

// route maps a path prefix to an upstream reverse proxy.
type route struct {
	prefix string
	exact  bool // match only the prefix itself, not sub-paths
	proxy  *httputil.ReverseProxy
}

// publicRoute describes a path reachable without authentication.
type publicRoute struct {
	method string // empty = any method
	path   string // exact path, or prefix when suffixed with "/"
	// optionalAuth: forward X-User-Id when a valid token is sent (still public).
	optionalAuth bool
}

// Gateway is the API gateway: it authenticates requests and reverse-proxies
// them to the account/asset services, following docs/api/openapi.yaml paths.
type Gateway struct {
	cfg    Config
	routes []route
	// announcements proxies ONLY the exact public path GET /announcements to
	// the admin service. No other admin path is reachable through the gateway.
	announcements *httputil.ReverseProxy
	public        []publicRoute
	// accessLog is nil when GATEWAY_ACCESS_LOG is off.
	accessLog *slog.Logger
	tradfi    *tradfiGuard
	// rdb backs the logout revocation check (nil = no check).
	rdb *redis.Client
}

// isLogout: POST /auth/logout is forwarded even with a missing/expired access
// token so the account service can revoke via the refreshToken in the body.
func isLogout(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/auth/logout"
}

// NewGateway builds a Gateway. It returns an error if an upstream URL is bad.
func NewGateway(cfg Config) (*Gateway, error) {
	account, err := url.Parse(cfg.AccountURL)
	if err != nil {
		return nil, err
	}
	asset, err := url.Parse(cfg.AssetURL)
	if err != nil {
		return nil, err
	}
	wallet, err := url.Parse(cfg.WalletURL)
	if err != nil {
		return nil, err
	}
	order, err := url.Parse(cfg.OrderURL)
	if err != nil {
		return nil, err
	}
	market, err := url.Parse(cfg.MarketURL)
	if err != nil {
		return nil, err
	}
	kyc, err := url.Parse(cfg.KycURL)
	if err != nil {
		return nil, err
	}
	accountProxy := httputil.NewSingleHostReverseProxy(account)
	assetProxy := httputil.NewSingleHostReverseProxy(asset)
	walletProxy := httputil.NewSingleHostReverseProxy(wallet)
	orderProxy := httputil.NewSingleHostReverseProxy(order)
	// ReverseProxy transparently tunnels WebSocket upgrades (used by /ws).
	marketProxy := httputil.NewSingleHostReverseProxy(market)
	kycProxy := httputil.NewSingleHostReverseProxy(kyc)
	futures, err := url.Parse(cfg.FuturesURL)
	if err != nil {
		return nil, err
	}
	futuresProxy := httputil.NewSingleHostReverseProxy(futures)
	strategy, err := url.Parse(cfg.StrategyURL)
	if err != nil {
		return nil, err
	}
	strategyProxy := httputil.NewSingleHostReverseProxy(strategy)
	growth, err := url.Parse(cfg.GrowthURL)
	if err != nil {
		return nil, err
	}
	growthProxy := httputil.NewSingleHostReverseProxy(growth)
	earn, err := url.Parse(cfg.EarnURL)
	if err != nil {
		return nil, err
	}
	earnProxy := httputil.NewSingleHostReverseProxy(earn)
	adminURL := cfg.AdminURL
	if adminURL == "" {
		adminURL = "http://localhost:8088"
	}
	admin, err := url.Parse(adminURL)
	if err != nil {
		return nil, err
	}
	annProxy := httputil.NewSingleHostReverseProxy(admin)
	annDirector := annProxy.Director
	annProxy.Director = func(r *http.Request) {
		annDirector(r)
		// Pin the upstream path and strip any credentials: the admin service
		// only ever sees an anonymous GET /announcements.
		r.URL.Path = "/announcements"
		r.URL.RawPath = ""
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		r.Header.Del("X-Internal-Token")
		r.Header.Del("X-User-Id")
		r.Header.Del("X-Admin-Token")
	}

	if _, err := parseCIDRs(cfg.TrustedProxyCIDRs); err != nil {
		return nil, err
	}
	return &Gateway{
		cfg:           cfg,
		announcements: annProxy,
		tradfi:        newTradFiGuard(cfg),
		// Order matters: longest/most-specific prefixes first.
		routes: []route{
			{prefix: "/auth", proxy: accountProxy},
			{prefix: "/2fa", proxy: accountProxy},
			// Fee rates are computed by the order service (spot + futures); must
			// precede the /account prefix.
			{prefix: "/account/fee-rates", proxy: orderProxy},
			{prefix: "/account", proxy: accountProxy},
			{prefix: "/notifications", proxy: accountProxy},
			{prefix: "/vip", proxy: accountProxy},
			// General transfer lives in the futures service (spot<->futures saga);
			// must precede the /assets prefix.
			{prefix: "/assets/transfer", proxy: futuresProxy},
			{prefix: "/assets/transfers", proxy: futuresProxy},
			{prefix: "/sub-accounts", proxy: accountProxy},
			{prefix: "/assets", proxy: assetProxy},
			{prefix: "/convert", proxy: assetProxy},
			{prefix: "/wallet", proxy: walletProxy},
			{prefix: "/kyc", proxy: kycProxy},
			{prefix: "/futures", proxy: futuresProxy},
			{prefix: "/event", proxy: futuresProxy}, // event contracts (futures service)
			{prefix: "/strategy", proxy: strategyProxy},
			{prefix: "/partner", proxy: growthProxy},
			{prefix: "/referral", proxy: growthProxy},
			{prefix: "/points", proxy: growthProxy},
			{prefix: "/rewards", proxy: growthProxy},
			{prefix: "/activities", proxy: growthProxy},
			{prefix: "/coupons", proxy: growthProxy},
			{prefix: "/loans", proxy: earnProxy},
			{prefix: "/earn", proxy: earnProxy},
			{prefix: "/spot", proxy: orderProxy},
			{prefix: "/market", proxy: marketProxy},
			// market-data only serves GET /ws; /ws/* is not a route.
			{prefix: "/ws", exact: true, proxy: marketProxy},
			{prefix: "/home/newcomer", proxy: growthProxy},              // newcomer task bar (growth)
			{prefix: "/growth/config", exact: true, proxy: growthProxy}, // public program numbers (growth)
			{prefix: "/app/features", proxy: marketProxy},               // app feature switches (market-data)
			{prefix: "/home", proxy: marketProxy},                       // home placements/banners + hot list (market-data)
			{prefix: "/chat", proxy: marketProxy},                       // trading chat rooms (market-data)
			{prefix: "/tradfi", proxy: marketProxy},                     // tokenized stocks / metals catalogue (market-data)
		},
		public: []publicRoute{
			{method: http.MethodGet, path: "/healthz"},
			{method: http.MethodPost, path: "/auth/code/send"},
			{method: http.MethodPost, path: "/auth/register"},
			{method: http.MethodPost, path: "/auth/login"},
			{method: http.MethodPost, path: "/auth/login/verify"},
			{method: http.MethodPost, path: "/auth/login/code"},
			{method: http.MethodPost, path: "/auth/refresh"},
			{method: http.MethodPost, path: "/auth/password/verify-code"},
			{method: http.MethodPost, path: "/auth/password/reset"},
			{method: http.MethodPost, path: "/auth/passkey/login/begin"},
			{method: http.MethodPost, path: "/auth/passkey/login/finish"},
			{method: http.MethodGet, path: "/account/avatars/"}, // public avatar retrieval (prefix)
			{method: "", path: "/market/"},                      // public market data (prefix)
			{method: http.MethodGet, path: "/futures/market/"},  // public futures market data (prefix)
			{method: http.MethodGet, path: "/futures/copy/summary"},
			{method: http.MethodGet, path: "/futures/copy/traders"},
			{method: http.MethodGet, path: "/futures/copy/traders/"}, // public trader profiles (prefix)
			{method: http.MethodGet, path: "/strategy/config"},
			{method: http.MethodGet, path: "/strategy/grid/square"}, // public strategy square
			{method: http.MethodGet, path: "/partner/rules"},
			{method: http.MethodGet, path: "/points/campaigns"},
			{method: http.MethodGet, path: "/points/campaign"},
			{method: http.MethodGet, path: "/activities"},
			{method: http.MethodGet, path: "/convert/pairs"},
			{method: http.MethodGet, path: "/loans/market"},
			{method: http.MethodGet, path: "/loans/quote"},
			{method: http.MethodGet, path: "/earn/products"},
			{method: http.MethodGet, path: "/earn/products/"}, // public product detail (prefix)
			{method: http.MethodGet, path: "/home/banners"},   // home carousel alias (read-only)
			{method: http.MethodGet, path: "/home/placements"},
			{method: http.MethodGet, path: "/app/features"}, // home operations slots (read-only)
			{method: http.MethodGet, path: "/home/newcomer", optionalAuth: true},
			{method: http.MethodGet, path: "/growth/config"},                     // invite/newcomer numbers (read-only)
			{method: http.MethodGet, path: "/home/hot"},                          // home hot list (read-only)
			{method: "", path: "/ws", optionalAuth: true},                        // market WebSocket; a token unlocks private channels (event:orders)
			{method: http.MethodGet, path: "/event/products"},                    // event contract products (public)
			{method: http.MethodGet, path: "/chat/rooms"},                        // chat: anyone can read
			{method: http.MethodGet, path: "/chat/rooms/"},                       // chat history (prefix); posting needs auth
			{method: http.MethodGet, path: "/tradfi/assets", optionalAuth: true}, // TradFi catalogue (flag + region gated)
		},
	}, nil
}

// Handler returns the gateway HTTP handler: access log / request id wraps
// CORS, which wraps auth + routing.
func (g *Gateway) Handler() http.Handler {
	return withAccessLog(g.accessLog, g.withCORS(http.HandlerFunc(g.serve)))
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Write([]byte("ok"))
		return
	}
	// Internal service-to-service endpoints and the admin back office are never
	// exposed through the public gateway: answer 404 so they are not discoverable.
	if hiddenPath(r.URL.Path) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
		return
	}

	// Public, read-only announcements (admin service public handler only).
	if r.URL.Path == "/announcements" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			common.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
			return
		}
		g.announcements.ServeHTTP(w, r)
		return
	}

	// Never trust a client-supplied identity header; the gateway is the sole
	// authority that sets it after validating the JWT.
	r.Header.Del("X-User-Id")
	r.Header.Del("X-Parent-Uid")
	r.Header.Del("X-Auth-Kind")

	// Sub-account API key (X-LB-APIKEY + X-LB-SECRET): verified by account,
	// then fenced by permission to trading/read paths only.
	if key := r.Header.Get(apiKeyHeader); key != "" {
		g.serveAPIKey(w, r, key)
		return
	}

	// Unknown routes answer 404 before auth (scanner noise such as /v1/*, /,
	// /markets no longer looks like "login required"). Known prefixes keep
	// their auth requirement below.
	proxy := g.match(r.URL.Path)
	if proxy == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
		return
	}

	if !g.isPublic(r) {
		token := bearer(r)
		var claims *common.AccessClaims
		var err error
		if token != "" {
			claims, err = common.ParseAccessToken(g.cfg.JWTSecret, token)
			if err == nil && g.tokenRevoked(r, claims) {
				claims, err = nil, common.ErrInvalidToken
			}
		}
		if claims == nil && isLogout(r) {
			// Account authenticates the bearer itself or falls back to the
			// body refreshToken; no identity header is set here.
			proxy.ServeHTTP(w, r)
			return
		}
		if token == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		if err != nil {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "令牌无效或已过期")
			return
		}
		if claims.Acct == "sub" {
			if !subSessionAllowed(r.Method, r.URL.Path) {
				common.WriteError(w, http.StatusForbidden, "SUB_ACCOUNT_FORBIDDEN", "子账户无权访问该功能")
				return
			}
			r.Header.Set("X-Parent-Uid", claims.Parent)
			r.Header.Set("X-Auth-Kind", "sub")
		}
		r.Header.Set("X-User-Id", claims.Subject)
	} else if g.optionalAuth(r) {
		// Public route that personalises when a valid token is present; a
		// missing or invalid token just means anonymous.
		token := bearer(r)
		if r.URL.Path == "/ws" {
			// Browsers cannot set headers on a WebSocket handshake: accept
			// ?token= and strip it before proxying.
			q := r.URL.Query()
			if token == "" {
				token = q.Get("token")
			}
			if q.Has("token") {
				q.Del("token")
				r.URL.RawQuery = q.Encode()
			}
		}
		if token != "" {
			if claims, err := common.ParseAccessToken(g.cfg.JWTSecret, token); err == nil && !g.tokenRevoked(r, claims) {
				r.Header.Set("X-User-Id", claims.Subject)
			}
		}
	}

	if g.tradfi != nil && !g.tradfi.check(w, r) {
		return
	}
	proxy.ServeHTTP(w, r)
}

// tokenRevoked reports whether the user logged out after the token was issued.
// A Redis error fails open (logged) so an outage does not lock users out.
func (g *Gateway) tokenRevoked(r *http.Request, c *common.AccessClaims) bool {
	revoked, err := common.AccessTokenRevoked(r.Context(), g.rdb, c)
	if err != nil {
		log.Printf("gateway: revocation check: %s", common.RedactSecrets(err.Error()))
	}
	return revoked
}

func (g *Gateway) optionalAuth(r *http.Request) bool {
	for _, p := range g.public {
		if p.optionalAuth && (p.method == "" || p.method == r.Method) && r.URL.Path == p.path {
			return true
		}
	}
	return false
}

func (g *Gateway) isPublic(r *http.Request) bool {
	for _, p := range g.public {
		if p.method != "" && p.method != r.Method {
			continue
		}
		if strings.HasSuffix(p.path, "/") {
			if strings.HasPrefix(r.URL.Path, p.path) {
				return true
			}
		} else if r.URL.Path == p.path {
			return true
		}
	}
	return false
}

func (g *Gateway) match(path string) *httputil.ReverseProxy {
	for _, rt := range g.routes {
		if path == rt.prefix || (!rt.exact && strings.HasPrefix(path, rt.prefix+"/")) {
			return rt.proxy
		}
	}
	return nil
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

// hiddenPath: /internal* and /admin* are not routed by the public gateway.
func hiddenPath(raw string) bool {
	p := strings.ToLower(path.Clean("/" + raw))
	return strings.HasPrefix(p, "/internal") || p == "/admin" || strings.HasPrefix(p, "/admin/")
}
