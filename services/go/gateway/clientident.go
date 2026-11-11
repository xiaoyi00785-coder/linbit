package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"strings"
)

// 最外层中间件：收成唯一的客户端 IP / 国家。
//
// 可以声明 IP 和国家的调用方：
//   - TCP 对端落在 TRUSTED_PROXY_CIDRS（docker 网桥上的 nginx，或直连的 Cloudflare）
//   - 或带 X-Linbit-Edge-Auth，且与 GATEWAY_EDGE_SECRET 常量时间相等。
//     web/partner 的 Next 直连 http://gateway:8080，容器 IP 不稳定，所以用共享密钥而不是地址。
//     密钥去空白后短于 32 字符则当作未设置。
//
// 其余请求删掉 X-Real-IP、X-Forwarded-For、CF-Connecting-IP、CF-IPCountry、Forwarded、True-Client-IP，
// 客户端就是 TCP 对端。通过之后这三个 IP 头只留一个校验过的地址，
// 地理封锁、API key 白名单、访问日志，以及读 X-Forwarded-For 第一段的 account/admin 看到同一值。
// X-Linbit-Edge-Auth 在比较后立即删除，不会转到上游。
const edgeAuthHeader = "X-Linbit-Edge-Auth"

// 短于该长度的 GATEWAY_EDGE_SECRET 忽略，按未配置处理。
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

// 只用于已信任的请求。顺序：配置的头、X-Real-IP、CF-Connecting-IP、X-Forwarded-For 第一段。取第一个合法 IP。
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
		// CIDR 与边缘密钥二选一。密钥未配置或对不上，就只认对端网段。
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

// trusted 表示上面接受了 CIDR 或边缘密钥。ok 为 false 表示中间件没跑（单测直调）。
func requestTrusted(r *http.Request) (trusted, ok bool) {
	v, ok := r.Context().Value(trustKey{}).(bool)
	return v, ok
}
