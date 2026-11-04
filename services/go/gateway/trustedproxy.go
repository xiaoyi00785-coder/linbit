package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// DefaultTrustedProxyCIDRs: Cloudflare edge ranges (https://www.cloudflare.com/ips/).
// CF-IPCountry is only trusted when the direct TCP peer is inside
// TRUSTED_PROXY_CIDRS; any other peer could forge the header.
const DefaultTrustedProxyCIDRs = "173.245.48.0/20,103.21.244.0/22,103.22.200.0/22,103.31.4.0/22,141.101.64.0/18," +
	"108.162.192.0/18,190.93.240.0/20,188.114.96.0/20,197.234.240.0/22,198.41.128.0/17,162.158.0.0/15," +
	"104.16.0.0/13,104.24.0.0/14,172.64.0.0/13,131.0.72.0/22," +
	"2400:cb00::/32,2606:4700::/32,2803:f800::/32,2405:b500::/32,2405:8100::/32,2a06:98c0::/29,2c0f:f248::/32"

func parseCIDRs(s string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") { // bare IP
			if ip := net.ParseIP(c); ip != nil && ip.To4() != nil {
				c += "/32"
			} else {
				c += "/128"
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS: invalid %q", c)
		}
		out = append(out, n)
	}
	return out, nil
}

func peerIP(r *http.Request) net.IP {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	return net.ParseIP(strings.Trim(h, "[]"))
}

// trustedCountry returns the CF-IPCountry value only when the direct peer is
// a trusted proxy; "" means unknown (untrusted peer, missing, XX or T1/Tor).
func trustedCountry(r *http.Request, trusted []*net.IPNet) string {
	ok, set := requestTrusted(r) // set by withClientIdentity (peer CIDR or edge secret)
	if !set {
		ok = peerTrusted(r, trusted)
	}
	if !ok {
		return ""
	}
	c := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	if len(c) != 2 || c == "XX" || c == "T1" {
		return ""
	}
	return c
}
