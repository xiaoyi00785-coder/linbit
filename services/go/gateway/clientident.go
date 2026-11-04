package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"strings"
)

// Client identity normalisation (outermost middleware).
//
// Who may assert the client's IP / country:
//   - a TCP peer inside TRUSTED_PROXY_CIDRS (nginx via the docker bridge, or
//     Cloudflare when the gateway is exposed directly), or
//   - a caller presenting X-Linbit-Edge-Auth == GATEWAY_EDGE_SECRET (the
//     web/partner Next servers calling http://gateway:8080 directly over the
//     docker network; their container IP is not stable, so a shared secret is
//     used instead of an address).
//
// For everyone else the identity headers (X-Real-IP, X-Forwarded-For,
// CF-Connecting-IP, CF-IPCountry, Forwarded, True-Client-IP) are deleted and
// the TCP peer is the client. Afterwards X-Real-IP / X-Forwarded-For carry
// exactly one validated IP, so the geo block, API-key IP allowlists, access
// log and downstream services (account/admin read X-Forwarded-For[0]) all see
// the same, unspoofable value. The edge-auth header never reaches upstreams.
const edgeAuthHeader = "X-Linbit-Edge-Auth"

// minEdgeSecretLen: shorter secrets are ignored (treated as unset).
const minEdgeSecretLen = 32

type trustKey struct{}

var identityHeaders = []string{"X-Real-IP", "X-Forwarded-For", "CF-Connecting-IP", "CF-IPCountry", "Forwarded", "True-Client-IP"}

func edgeSecretUsable(s string) bool { return len(strings.TrimSpace(s)) >= minEdgeSecretLen }

func peerTrusted(r *http.Request, trusted []*net.IPNet) bool {
	ip := peerIP(r)
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func validIP(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "[]")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return ""
}

// resolveClientIP picks the client IP from a trusted request's headers.
func resolveClientIP(r *http.Request, hdr string) string {
	cands := []string{}
	if hdr != "" {
		cands = append(cands, r.Header.Get(hdr))
	}
	cands = append(cands, r.Header.Get("X-Real-IP"), r.Header.Get("CF-Connecting-IP"))
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		cands = append(cands, strings.Split(xff, ",")[0])
	}
	for _, c := range cands {
		if v := validIP(c); v != "" {
			return v
		}
	}
	return ""
}

func withClientIdentity(trusted []*net.IPNet, edgeSecret, ipHeader string, next http.Handler) http.Handler {
	secret := strings.TrimSpace(edgeSecret)
	if secret != "" && !edgeSecretUsable(secret) {
		log.Printf("gateway: GATEWAY_EDGE_SECRET shorter than %d chars, ignored", minEdgeSecretLen)
		secret = ""
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get(edgeAuthHeader)
		r.Header.Del(edgeAuthHeader)
		ok := peerTrusted(r, trusted)
		if !ok && secret != "" && got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1 {
			ok = true
		}
		ip := ""
		if ok {
			ip = resolveClientIP(r, ipHeader)
		}
		country := ""
		if ok {
			country = r.Header.Get("CF-IPCountry")
		}
		for _, h := range identityHeaders {
			r.Header.Del(h)
		}
		if ip == "" {
			if p := peerIP(r); p != nil {
				ip = p.String()
			}
		}
		if ip != "" {
			r.Header.Set("X-Real-IP", ip)
			r.Header.Set("X-Forwarded-For", ip)
			r.Header.Set("CF-Connecting-IP", ip)
			if ipHeader != "" {
				r.Header.Set(ipHeader, ip)
			}
		}
		if country != "" {
			r.Header.Set("CF-IPCountry", country)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), trustKey{}, ok)))
	})
}

// requestTrusted reports whether withClientIdentity accepted the request's
// edge headers; ok=false when the middleware did not run (direct unit tests).
func requestTrusted(r *http.Request) (trusted, ok bool) {
	v, ok := r.Context().Value(trustKey{}).(bool)
	return v, ok
}
