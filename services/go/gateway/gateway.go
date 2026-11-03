package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// route maps a path prefix to an upstream reverse proxy.
type route struct {
	prefix string
	proxy  *httputil.ReverseProxy
}

// publicRoute describes a path reachable without authentication.
type publicRoute struct {
	method string // empty = any method
	path   string // exact path, or prefix when suffixed with "/"
}

// Gateway is the API gateway: it authenticates requests and reverse-proxies
// them to the account/asset services, following docs/api/openapi.yaml paths.
type Gateway struct {
	cfg    Config
	routes []route
	public []publicRoute
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
	order, err := url.Parse(cfg.OrderURL)
	if err != nil {
		return nil, err
	}
	accountProxy := httputil.NewSingleHostReverseProxy(account)
	assetProxy := httputil.NewSingleHostReverseProxy(asset)
	orderProxy := httputil.NewSingleHostReverseProxy(order)

	return &Gateway{
		cfg: cfg,
		// Order matters: longest/most-specific prefixes first.
		routes: []route{
			{prefix: "/auth", proxy: accountProxy},
			{prefix: "/2fa", proxy: accountProxy},
			{prefix: "/account", proxy: accountProxy},
			{prefix: "/assets", proxy: assetProxy},
			{prefix: "/wallet", proxy: assetProxy},
			{prefix: "/spot", proxy: orderProxy},
		},
		public: []publicRoute{
			{method: http.MethodGet, path: "/healthz"},
			{method: http.MethodPost, path: "/auth/register"},
			{method: http.MethodPost, path: "/auth/login"},
			{method: http.MethodPost, path: "/auth/refresh"},
			{method: "", path: "/market/"}, // public market data (prefix)
		},
	}, nil
}

// Handler returns the gateway HTTP handler (CORS wraps auth + routing).
func (g *Gateway) Handler() http.Handler {
	return g.withCORS(http.HandlerFunc(g.serve))
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Write([]byte("ok"))
		return
	}
	// Internal service-to-service endpoints are never exposed through the gateway.
	if strings.HasPrefix(r.URL.Path, "/internal") {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
		return
	}

	// Never trust a client-supplied identity header; the gateway is the sole
	// authority that sets it after validating the JWT.
	r.Header.Del("X-User-Id")

	if !g.isPublic(r) {
		token := bearer(r)
		if token == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		claims, err := common.ParseAccessToken(g.cfg.JWTSecret, token)
		if err != nil {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "令牌无效或已过期")
			return
		}
		r.Header.Set("X-User-Id", claims.Subject)
	}

	proxy := g.match(r.URL.Path)
	if proxy == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
		return
	}
	proxy.ServeHTTP(w, r)
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
		if path == rt.prefix || strings.HasPrefix(path, rt.prefix+"/") {
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
