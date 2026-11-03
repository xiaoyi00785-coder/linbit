package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// apikey.go: sub-account API keys and sub-account session fencing.
//
// A sub-account must never reach parent-only endpoints (/sub-accounts/*) or
// any deposit/withdraw endpoint. API keys are additionally limited to the
// read / spot / futures surfaces their permissions grant.

const (
	apiKeyHeader    = "X-LB-APIKEY"
	apiSecretHeader = "X-LB-SECRET"
)

func cleanPath(p string) string { return path.Clean("/" + p) }

func hasPrefixSeg(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

// alwaysDeniedForSub: deposit/withdraw and parent-management surfaces.
func alwaysDeniedForSub(p string) bool {
	return hasPrefixSeg(p, "/wallet") ||
		hasPrefixSeg(p, "/account/withdraw-whitelist") ||
		hasPrefixSeg(p, "/account/withdraw-addresses") ||
		hasPrefixSeg(p, "/sub-accounts")
}

// subSessionAllowed fences a logged-in sub-account (JWT acct=sub). The only
// /sub-accounts call it may make is sending funds back to its parent, and it
// may not disable its 2FA.
func subSessionAllowed(method, raw string) bool {
	p := cleanPath(raw)
	if method == http.MethodPost && p == "/sub-accounts/transfer" {
		return true
	}
	if p == "/2fa/totp/disable" || hasPrefixSeg(p, "/kyc") {
		return false
	}
	return !alwaysDeniedForSub(p)
}

// apiKeyAllowed: read = GET/HEAD on /assets, /spot, /futures; spot_trade =
// any method on /spot; futures_trade = any method on /futures. Everything else
// (account, auth, 2fa, wallet, sub-accounts, earn, ...) is denied.
func apiKeyAllowed(perms []string, method, raw string) bool {
	p := cleanPath(raw)
	if alwaysDeniedForSub(p) {
		return false
	}
	// Futures account transfers move funds between the sub's own wallets: allowed only with futures_trade.
	has := map[string]bool{}
	for _, x := range perms {
		has[x] = true
	}
	read := method == http.MethodGet || method == http.MethodHead
	switch {
	case hasPrefixSeg(p, "/spot"):
		return has["spot_trade"] || (read && has["read"])
	case hasPrefixSeg(p, "/futures"):
		return has["futures_trade"] || (read && has["read"])
	case hasPrefixSeg(p, "/assets"):
		return read && has["read"]
	case hasPrefixSeg(p, "/market"):
		return read
	}
	return false
}

func (g *Gateway) clientIP(r *http.Request) string {
	if h := g.cfg.ClientIPHeader; h != "" {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

var keyHTTP = &http.Client{Timeout: 3 * time.Second}

type keyVerdict struct {
	OK          bool     `json:"ok"`
	Reason      string   `json:"reason"`
	UID         string   `json:"uid"`
	ParentUID   string   `json:"parentUid"`
	Permissions []string `json:"permissions"`
}

func (g *Gateway) verifyKey(r *http.Request, key, secret string) (*keyVerdict, error) {
	b, _ := json.Marshal(map[string]string{"apiKey": key, "apiSecret": secret, "ip": g.clientIP(r)})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(g.cfg.AccountURL, "/")+"/internal/api-keys/verify", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", g.cfg.InternalToken)
	resp, err := keyHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var v keyVerdict
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&v) != nil {
		return nil, errUpstream
	}
	return &v, nil
}

type gwErr string

func (e gwErr) Error() string { return string(e) }

const errUpstream = gwErr("upstream")

func (g *Gateway) serveAPIKey(w http.ResponseWriter, r *http.Request, key string) {
	secret := r.Header.Get(apiSecretHeader)
	r.Header.Del(apiKeyHeader)
	r.Header.Del(apiSecretHeader)
	r.Header.Del("Authorization")
	if secret == "" || g.cfg.InternalToken == "" {
		common.WriteError(w, http.StatusUnauthorized, "API_KEY_INVALID", "API Key 无效")
		return
	}
	// Cheap pre-check: never even verify keys for denied or non-canonical paths.
	if cleanPath(r.URL.Path) != r.URL.Path || alwaysDeniedForSub(r.URL.Path) {
		common.WriteError(w, http.StatusForbidden, "API_KEY_FORBIDDEN", "API Key 无权访问该接口")
		return
	}
	v, err := g.verifyKey(r, key, secret)
	if err != nil {
		common.WriteError(w, http.StatusServiceUnavailable, "UPSTREAM_ERROR", "鉴权服务暂不可用")
		return
	}
	if !v.OK {
		switch v.Reason {
		case "ip_not_allowed":
			common.WriteError(w, http.StatusForbidden, "API_KEY_IP_DENIED", "当前 IP 不在 API Key 白名单内")
		case "account_frozen":
			common.WriteError(w, http.StatusForbidden, "ACCOUNT_FROZEN", "账户已冻结")
		default:
			common.WriteError(w, http.StatusUnauthorized, "API_KEY_INVALID", "API Key 无效")
		}
		return
	}
	if !apiKeyAllowed(v.Permissions, r.Method, r.URL.Path) {
		common.WriteError(w, http.StatusForbidden, "API_KEY_FORBIDDEN", "API Key 无权访问该接口")
		return
	}
	r.Header.Set("X-User-Id", v.UID)
	r.Header.Set("X-Parent-Uid", v.ParentUID)
	r.Header.Set("X-Auth-Kind", "apikey")
	proxy := g.match(r.URL.Path)
	if proxy == nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
		return
	}
	// TradFi region/feature gate applies to API-key traffic too.
	if g.tradfi != nil && !g.tradfi.check(w, r) {
		return
	}
	proxy.ServeHTTP(w, r)
}
