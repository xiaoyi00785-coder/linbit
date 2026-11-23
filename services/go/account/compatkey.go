package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
)

// compatKeyHeader carries the shared secret easicoin-compat attaches to the
// requests it sends to the gateway over loopback.
const compatKeyHeader = "X-Linbit-Compat-Key"

// minCompatKeyLen rejects weak or truncated secrets; a shorter key disables
// the compat path entirely (every new device gets the normal OTP).
const minCompatKeyLen = 32

// compatSharedKey is ACCOUNT_COMPAT_SHARED_KEY. Tests override it.
var compatSharedKey = strings.TrimSpace(os.Getenv("ACCOUNT_COMPAT_SHARED_KEY"))

// skipNewDeviceOTP is true only for requests that really come from
// easicoin-compat. That BFF cannot surface Linbit's login challenge to the
// EasiCoin app, so it runs its own mailed login code (consumeLoginMailCode)
// before it hands any token to the app; account therefore lets it skip the
// duplicate new-device OTP.
//
// The User-Agent is NOT trusted any more: anyone could append
// "EasiCoin-compat" to theirs and skip the new-device check. A request
// qualifies only when it carries the shared secret (constant-time compare)
// AND arrives from a private/loopback peer (the gateway on the docker
// network). Without a configured key nothing qualifies.
func skipNewDeviceOTP(r *http.Request) bool {
	key := compatSharedKey
	if len(key) < minCompatKeyLen {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(compatKeyHeader))
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
		return false
	}
	return privatePeer(r)
}

func privatePeer(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}
