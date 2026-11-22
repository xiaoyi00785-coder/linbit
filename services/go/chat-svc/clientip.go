package main

import (
	"net"
	"net/http"
	"strings"
)

// Client IP resolution (guest anti-abuse + cs_conversations.ip audit).
//
// Chain: Cloudflare -> nginx (real_ip from CF ranges only; overwrites
// X-Real-IP/X-Forwarded-For) -> compat (loopback; forwards nginx's X-Real-IP)
// -> gateway (trusts only TRUSTED_PROXY_CIDRS peers, normalises X-Real-IP /
// X-Forwarded-For to one validated IP) -> chat-svc.
//
// chat-svc is reachable only from the docker network (gateway) and the
// loopback publish (compat workbench). The identity headers are honoured only
// when the direct TCP peer is in trustedProxyNets; any other peer is the client.
const defaultTrustedProxyCIDRs = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

var trustedProxyNets = parseCIDRList(defaultTrustedProxyCIDRs)

func parseCIDRList(s string) []*net.IPNet {
	var out []*net.IPNet
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			if strings.Contains(p, ":") {
				p += "/128"
			} else {
				p += "/32"
			}
		}
		if _, n, err := net.ParseCIDR(p); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func validIP(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "[]")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return ""
}

func peerAddr(r *http.Request) net.IP {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	return net.ParseIP(strings.Trim(h, "[]"))
}

func ipTrusted(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP returns the client IP: X-Real-IP (then the first X-Forwarded-For
// entry) only from a trusted direct peer, otherwise the TCP peer itself.
func clientIP(r *http.Request) string {
	peer := peerAddr(r)
	if peer != nil && ipTrusted(peer, trustedProxyNets) {
		if v := validIP(r.Header.Get("X-Real-IP")); v != "" {
			return v
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if v := validIP(strings.Split(xff, ",")[0]); v != "" {
				return v
			}
		}
	}
	if peer != nil {
		return peer.String()
	}
	return r.RemoteAddr
}
