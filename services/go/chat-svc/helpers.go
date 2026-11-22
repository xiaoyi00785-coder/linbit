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
