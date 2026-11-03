package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectImageType(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		wantExt string
		wantOK  bool
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, "jpg", true},
		{"png", append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, 0x00), "png", true},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp", true},
		{"gif-unsupported", []byte("GIF89a......"), "", false},
		{"text", []byte("hello world not an image"), "", false},
		{"empty", []byte{}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it, err := DetectImageType(c.data)
			if c.wantOK && (err != nil || it.Ext != c.wantExt) {
				t.Fatalf("DetectImageType(%s) = (%+v,%v), want ext=%s ok", c.name, it, err, c.wantExt)
			}
			if !c.wantOK && err == nil {
				t.Fatalf("DetectImageType(%s) accepted unsupported data", c.name)
			}
		})
	}
}

func TestLocalStoragePutOpenURL(t *testing.T) {
	dir := t.TempDir()
	ls, err := NewLocalStorage(dir, "/account/avatars")
	if err != nil {
		t.Fatalf("new local storage: %v", err)
	}
	key := "avatars/80000001/abc123.png"
	payload := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("body")...)
	url, err := ls.Put(context.Background(), key, "image/png", payload)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if want := "/account/avatars/" + key; url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
	// File exists on disk under baseDir.
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// Open returns the bytes and the extension-derived content type.
	body, ct, err := ls.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != string(payload) {
		t.Fatalf("roundtrip mismatch")
	}
	if ct != "image/png" {
		t.Fatalf("content type = %q, want image/png", ct)
	}
}

func TestLocalStorageOpenMissing(t *testing.T) {
	ls, _ := NewLocalStorage(t.TempDir(), "/x")
	if _, _, err := ls.Open(context.Background(), "nope/missing.png"); err != ErrNotFound {
		t.Fatalf("missing key err = %v, want ErrNotFound", err)
	}
}

func TestLocalStorageTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	ls, _ := NewLocalStorage(dir, "/x")
	// Attempt to escape via "..": cleanKey must neutralise it so the write lands
	// inside baseDir, never above it.
	url, err := ls.Put(context.Background(), "../../etc/evil.png", "image/png", []byte("x"))
	if err != nil {
		t.Fatalf("put with traversal key: %v", err)
	}
	// The escaping components are stripped; resulting file stays under baseDir.
	if _, err := os.Stat(filepath.Join(dir, "etc", "evil.png")); err != nil {
		t.Fatalf("traversal not contained inside baseDir: %v", err)
	}
	_ = url
}

func TestLocalStorageServeHTTP(t *testing.T) {
	dir := t.TempDir()
	ls, _ := NewLocalStorage(dir, "/account/avatars")
	key := "avatars/u/pic.webp"
	payload := []byte("RIFF\x00\x00\x00\x00WEBP....")
	if _, err := ls.Put(context.Background(), key, "image/webp", payload); err != nil {
		t.Fatalf("put: %v", err)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ls.ServeHTTP(w, r, "/account/avatars")
	})
	// Existing object.
	r := httptest.NewRequest("GET", "/account/avatars/"+key, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("serve status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("content type = %q", ct)
	}
	if w.Body.String() != string(payload) {
		t.Fatalf("served body mismatch")
	}
	// Missing object -> 404.
	r2 := httptest.NewRequest("GET", "/account/avatars/avatars/u/missing.png", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("missing serve status = %d, want 404", w2.Code)
	}
}

func TestS3PutOpenRoundTrip(t *testing.T) {
	objs := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AK/") || r.Header.Get("X-Amz-Content-Sha256") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			objs[r.URL.Path] = b
		case http.MethodGet:
			b, ok := objs[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(b)
		}
	}))
	defer srv.Close()
	s := NewS3Storage(S3Config{Bucket: "b", Endpoint: srv.URL, AccessKeyID: "AK", SecretAccessKey: "SK"})
	ref, err := s.Put(context.Background(), "kyc/u1/a b.png", "image/png", []byte("img"))
	if err != nil || ref != "kyc/u1/a b.png" {
		t.Fatalf("put = %q, %v", ref, err)
	}
	rc, ct, err := s.Open(context.Background(), "kyc/u1/a b.png")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "img" || ct != "image/png" {
		t.Fatalf("got %q %q", b, ct)
	}
	if _, _, err := s.Open(context.Background(), "missing"); err != ErrNotFound {
		t.Fatalf("missing err = %v", err)
	}
}

func TestS3Unconfigured(t *testing.T) {
	t.Setenv("S3_ACCESS_KEY_ID", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	s := NewS3Storage(S3Config{Bucket: "b"})
	if _, err := s.Put(context.Background(), "k", "image/png", []byte("x")); err == nil {
		t.Fatal("want error without credentials")
	}
}
