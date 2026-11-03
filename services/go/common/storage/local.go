package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalStorage stores objects as files under BaseDir and exposes them through a
// controlled HTTP route rooted at PublicBase.
//
// A stored key "avatars/80000001/ab12cd.png" becomes the file
// <BaseDir>/avatars/80000001/ab12cd.png and the URL
// <PublicBase>/avatars/80000001/ab12cd.png. Keys are sanitised on both write
// and read so a request can never escape BaseDir via "..".
type LocalStorage struct {
	baseDir    string // filesystem root (created if missing)
	publicBase string // URL prefix for retrieval, e.g. "/account/avatars" or "https://cdn/x"
}

var _ ObjectStorage = (*LocalStorage)(nil)

// NewLocalStorage creates a LocalStorage, ensuring baseDir exists. publicBase is
// the URL prefix prepended to keys to form retrieval URLs (no trailing slash
// required; one is handled).
func NewLocalStorage(baseDir, publicBase string) (*LocalStorage, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("storage: baseDir is required")
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create baseDir: %w", err)
	}
	return &LocalStorage{baseDir: abs, publicBase: strings.TrimRight(publicBase, "/")}, nil
}

// Put writes data to the file for key and returns its public URL. The on-disk
// directory tree is created as needed. contentType is accepted for interface
// parity; the local backend re-derives it from the key extension when serving.
func (l *LocalStorage) Put(_ context.Context, key, _ string, data []byte) (string, error) {
	full, err := l.resolve(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return l.publicBase + "/" + cleanKey(key), nil
}

// Open returns a reader for key plus its content type (derived from the key
// extension). Returns ErrNotFound when the file is missing.
func (l *LocalStorage) Open(_ context.Context, key string) (io.ReadCloser, string, error) {
	full, err := l.resolve(key)
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(full)), ".")
	return f, mimeByExt(ext), nil
}

// ServeHTTP streams the object addressed by the request path relative to prefix.
// It is the "controlled route" that backs public retrieval URLs: it only serves
// files under BaseDir (traversal is blocked by resolve) and sets Content-Type
// from the stored extension. Mount it so that, e.g., prefix "/account/avatars"
// handles "GET /account/avatars/{rest...}".
func (l *LocalStorage) ServeHTTP(w http.ResponseWriter, r *http.Request, prefix string) {
	rel := strings.TrimPrefix(r.URL.Path, strings.TrimRight(prefix, "/")+"/")
	body, contentType, err := l.Open(r.Context(), rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, body)
}

// resolve maps a key to an absolute path inside baseDir, rejecting any key that
// would escape the root.
func (l *LocalStorage) resolve(key string) (string, error) {
	clean := cleanKey(key)
	if clean == "" {
		return "", fmt.Errorf("storage: empty key")
	}
	full := filepath.Join(l.baseDir, filepath.FromSlash(clean))
	// Defence in depth: ensure the joined path stays within baseDir.
	if full != l.baseDir && !strings.HasPrefix(full, l.baseDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: key escapes base directory")
	}
	return full, nil
}

// cleanKey normalises a slash-separated key and strips any leading "/" or ".."
// components so it can never climb above the root.
func cleanKey(key string) string {
	clean := path.Clean("/" + strings.TrimSpace(key)) // leading slash absorbs ".."
	return strings.TrimPrefix(clean, "/")
}
