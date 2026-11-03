package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// S3Config holds the settings for an S3 (or S3-compatible, e.g. MinIO, R2)
// backend. Requests use path-style addressing and AWS Signature V4.
type S3Config struct {
	Bucket     string // target bucket
	Region     string // AWS region (or endpoint region for compatibles; default us-east-1)
	Endpoint   string // custom endpoint for S3-compatible stores; empty = AWS (https://s3.<region>.amazonaws.com)
	PublicBase string // public URL prefix returned by Put (CDN or bucket URL); empty = key only
	// Credentials; when empty they are read from S3_ACCESS_KEY_ID /
	// S3_SECRET_ACCESS_KEY (falling back to AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY).
	AccessKeyID     string
	SecretAccessKey string
	HTTPClient      *http.Client
	now             func() time.Time
}

// S3Storage is an ObjectStorage backed by S3-compatible object storage,
// implemented without the AWS SDK (PUT/GET object with SigV4).
type S3Storage struct {
	cfg S3Config
}

var _ ObjectStorage = (*S3Storage)(nil)

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// NewS3Storage constructs an S3Storage from cfg.
func NewS3Storage(cfg S3Config) *S3Storage {
	if cfg.AccessKeyID == "" {
		cfg.AccessKeyID = firstEnv("S3_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID")
	}
	if cfg.SecretAccessKey == "" {
		cfg.SecretAccessKey = firstEnv("S3_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://s3." + cfg.Region + ".amazonaws.com"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &S3Storage{cfg: cfg}
}

func (s *S3Storage) objectURL(key string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(s.cfg.Endpoint, "/"))
	if err != nil {
		return nil, err
	}
	segs := []string{s.cfg.Bucket}
	for _, p := range strings.Split(strings.TrimLeft(key, "/"), "/") {
		segs = append(segs, p)
	}
	esc := make([]string, len(segs))
	for i, p := range segs {
		esc[i] = uriEncode(p)
	}
	u.RawPath = u.Path + "/" + strings.Join(esc, "/")
	u.Path = u.Path + "/" + strings.Join(segs, "/")
	return u, nil
}

// uriEncode encodes per the SigV4 rules (unreserved chars kept).
func uriEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func sha256Hex(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func hmacSHA256(k []byte, s string) []byte { m := hmac.New(sha256.New, k); m.Write([]byte(s)); return m.Sum(nil) }

func (s *S3Storage) sign(req *http.Request, payloadHash string) {
	t := s.cfg.now().UTC()
	amz := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	req.Header.Set("X-Amz-Date", amz)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	host := req.URL.Host
	signed := "host;x-amz-content-sha256;x-amz-date"
	canonHeaders := "host:" + host + "\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amz + "\n"
	canon := strings.Join([]string{req.Method, req.URL.EscapedPath(), "", canonHeaders, signed, payloadHash}, "\n")
	scope := day + "/" + s.cfg.Region + "/s3/aws4_request"
	sts := "AWS4-HMAC-SHA256\n" + amz + "\n" + scope + "\n" + sha256Hex([]byte(canon))
	k := hmacSHA256([]byte("AWS4"+s.cfg.SecretAccessKey), day)
	k = hmacSHA256(k, s.cfg.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, sts))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.cfg.AccessKeyID+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

// Put uploads data to Bucket/<key> and returns PublicBase + "/" + key
// (or the key itself when PublicBase is empty).
func (s *S3Storage) Put(ctx context.Context, key, contentType string, data []byte) (string, error) {
	if s.cfg.Bucket == "" || s.cfg.AccessKeyID == "" || s.cfg.SecretAccessKey == "" {
		return "", fmt.Errorf("storage: s3 not configured (bucket/credentials)")
	}
	u, err := s.objectURL(key)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.ContentLength = int64(len(data))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	s.sign(req, sha256Hex(data))
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("storage: s3 put HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if s.cfg.PublicBase == "" {
		return key, nil
	}
	return strings.TrimRight(s.cfg.PublicBase, "/") + "/" + strings.TrimLeft(key, "/"), nil
}

// Open streams Bucket/<key> back (used for private objects such as KYC
// documents served through the admin service). ErrNotFound on 404.
func (s *S3Storage) Open(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if s.cfg.Bucket == "" || s.cfg.AccessKeyID == "" || s.cfg.SecretAccessKey == "" {
		return nil, "", fmt.Errorf("storage: s3 not configured (bucket/credentials)")
	}
	u, err := s.objectURL(key)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	s.sign(req, sha256Hex(nil))
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, "", ErrNotFound
	case resp.StatusCode/100 != 2:
		resp.Body.Close()
		return nil, "", fmt.Errorf("storage: s3 get HTTP %d", resp.StatusCode)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}
