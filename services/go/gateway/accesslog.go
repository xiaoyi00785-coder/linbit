package main

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Structured access log for the gateway (GATEWAY_ACCESS_LOG, default on).
//
// One JSON line per request: method, host, path, redacted query, status,
// latency, bytes, client ip (CF-Connecting-IP > X-Forwarded-For[0] >
// X-Real-IP > RemoteAddr), request id, user agent, and user id when the JWT
// was valid. Health checks are skipped. Query values whose key looks like a
// credential are replaced by "***"; Authorization/cookies are never logged.

const requestIDHeader = "X-Request-Id"

var reValidReqID = regexp.MustCompile(`^[A-Za-z0-9._:\-]{1,64}$`)

// sensitiveQueryKey reports whether a query parameter must be masked.
func sensitiveQueryKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range []string{"token", "secret", "password", "passwd", "pwd", "key", "sign", "signature", "otp", "totp", "code", "auth", "session", "jwt", "ticket"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// redactQuery masks credential-like query values; unparsable queries are dropped.
func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return "<unparsable>"
	}
	for k := range vals {
		if sensitiveQueryKey(k) {
			for i := range vals[k] {
				vals[k][i] = "***"
			}
		}
	}
	return strings.ReplaceAll(vals.Encode(), "%2A%2A%2A", "***")
}

// clientIP picks the end-user address behind Cloudflare / nginx.
func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if first := strings.TrimSpace(strings.Split(v, ",")[0]); first != "" {
			return first
		}
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// ensureRequestID reuses a sane inbound X-Request-Id (e.g. nginx $request_id)
// or mints one, forwards it upstream and echoes it on the response.
func ensureRequestID(w http.ResponseWriter, r *http.Request) string {
	id := r.Header.Get(requestIDHeader)
	if !reValidReqID.MatchString(id) {
		id = common.RequestID()
	}
	r.Header.Set(requestIDHeader, id)
	w.Header().Set(requestIDHeader, id)
	return id
}

// statusRecorder captures status/bytes while still supporting WebSocket
// hijacking and streaming flushes.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	if s.status == 0 {
		s.status = http.StatusSwitchingProtocols
	}
	return h.Hijack()
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// accessLogEnabled reads GATEWAY_ACCESS_LOG (default on; 0/false/off disables).
func accessLogEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(common.Env("GATEWAY_ACCESS_LOG", "true"))) {
	case "0", "false", "off", "no":
		return false
	}
	return true
}

func newAccessLogger(w io.Writer) *slog.Logger {
	if w == nil {
		w = common.RedactingWriter(os.Stdout)
	}
	return slog.New(slog.NewJSONHandler(w, nil))
}

// withAccessLog wraps next with request-id propagation and (if logger != nil)
// one structured log line per non-health request.
func withAccessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ensureRequestID(w, r)
		if logger == nil || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		attrs := []any{
			"method", r.Method,
			"host", r.Host,
			"path", r.URL.Path,
			"status", rec.status,
			"latency_ms", float64(time.Since(start).Microseconds()) / 1000,
			"bytes", rec.bytes,
			"ip", clientIP(r),
			"request_id", id,
			"ua", r.UserAgent(),
		}
		if q := redactQuery(r.URL.RawQuery); q != "" {
			attrs = append(attrs, "query", q)
		}
		if uid := r.Header.Get("X-User-Id"); uid != "" {
			attrs = append(attrs, "uid", uid)
		}
		if r.Header.Get("Upgrade") != "" {
			attrs = append(attrs, "upgrade", r.Header.Get("Upgrade"))
		}
		logger.Info("http", attrs...)
	})
}
