package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/linbit/platform/services/go/common"
)

// atoiDefault parses s as an int, clamping to [min,max], using def when empty/bad.
func atoiDefault(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// intErr logs and emits a generic 500.
func intErr(w http.ResponseWriter, what string, err error) {
	log.Printf("chat-svc %s: %v", what, err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

// clientIP extracts the best-effort client IP for guest anti-abuse.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := indexByte(v, ','); i >= 0 {
			return trimSpace(v[:i])
		}
		return trimSpace(v)
	}
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	host := r.RemoteAddr
	if i := lastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// saveUpload streams an uploaded file to dir/name, capping bytes.
func saveUpload(dir, name string, src io.Reader, max int64) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, io.LimitReader(src, max))
	return err
}
