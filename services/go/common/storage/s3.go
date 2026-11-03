package storage

import (
	"context"
	"io"
)

// S3Config holds the settings a real S3 (or S3-compatible, e.g. MinIO) backend
// needs. Kept here so wiring/config can be added without reshaping callers.
type S3Config struct {
	Bucket    string // target bucket
	Region    string // AWS region (or endpoint region for compatibles)
	Endpoint  string // custom endpoint for S3-compatible stores; empty = AWS
	PublicBase string // public URL prefix for retrieval (CDN or bucket URL)
	// Credentials are intentionally omitted here; a real implementation should
	// source them from the environment / instance role / a secrets manager.
}

// S3Storage is a skeleton ObjectStorage backed by S3-compatible object storage.
//
// It deliberately does NOT pull in the AWS SDK yet: the methods return
// ErrNotImplemented so the interface compiles and callers can select the backend
// by config, while the actual PutObject/GetObject calls are filled in later.
type S3Storage struct {
	cfg S3Config
}

var _ ObjectStorage = (*S3Storage)(nil)

// NewS3Storage constructs an S3Storage from cfg. The returned value satisfies
// ObjectStorage but its operations are not yet functional (see the type doc).
func NewS3Storage(cfg S3Config) *S3Storage {
	return &S3Storage{cfg: cfg}
}

// Put is not yet implemented. A real version uploads data to
// cfg.Bucket/<key> with the given Content-Type and returns
// cfg.PublicBase + "/" + key.
func (s *S3Storage) Put(_ context.Context, key, _ string, _ []byte) (string, error) {
	// TODO: s3.PutObject(bucket=s.cfg.Bucket, key=key, body=data, contentType=contentType)
	//       then return s.cfg.PublicBase + "/" + key, nil
	return "", ErrNotImplemented
}

// Open is not yet implemented. For S3 the public URL returned by Put is usually
// fetched directly by the client/CDN rather than streamed back through the
// service, so this may stay unimplemented.
func (s *S3Storage) Open(_ context.Context, _ string) (io.ReadCloser, string, error) {
	// TODO: s3.GetObject(bucket=s.cfg.Bucket, key=key)
	return nil, "", ErrNotImplemented
}
