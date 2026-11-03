package main

import (
	"net/http"
	"net/url"
	"strings"
)

// CORS handling for the gateway.
//
// Allowed origins come from CORS_ALLOWED_ORIGINS (comma-separated). An entry may
// be an exact origin ("https://app.linbit.com"), a trailing-"*" prefix pattern
// ("http://localhost:*"), or "*" to allow any origin. When the list is empty
// (unconfigured), a development default allows http://localhost:* and
// http://127.0.0.1:* on any port so local frontends work out of the box.
//
// Preflight (OPTIONS) requests are answered here; the allowed request headers
// always include Authorization and Content-Type.

const corsAllowHeaders = "Authorization, Content-Type"
const corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

// splitOrigins parses a comma-separated origin list, trimming blanks.
func splitOrigins(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// originAllowed reports whether an Origin header value is permitted.
func (g *Gateway) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	if len(g.cfg.AllowedOrigins) == 0 {
		return devDefaultOrigin(origin)
	}
	for _, pat := range g.cfg.AllowedOrigins {
		if originMatches(pat, origin) {
			return true
		}
	}
	return false
}

// originMatches matches an origin against a configured pattern: "*" (any),
// trailing "*" (prefix), or exact.
func originMatches(pattern, origin string) bool {
	switch {
	case pattern == "*":
		return true
	case strings.HasSuffix(pattern, "*"):
		return strings.HasPrefix(origin, strings.TrimSuffix(pattern, "*"))
	default:
		return pattern == origin
	}
}

// devDefaultOrigin allows http://localhost and http://127.0.0.1 on any port.
func devDefaultOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1"
}

// withCORS wraps a handler with CORS response headers and preflight handling.
func (g *Gateway) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := g.originAllowed(origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		// Preflight: short-circuit before auth/routing (it carries no credentials).
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
