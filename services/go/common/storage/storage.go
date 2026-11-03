// Package storage is a pluggable binary object store shared by the Linbit Go
// services. Avatars use it today; KYC document uploads are expected to reuse the
// same interface later (just a different backend instance / key prefix).
//
// An ObjectStorage persists a blob under a caller-chosen key and reports the
// public URL at which it can be retrieved. Two backends ship here:
//
//   - LocalStorage writes blobs to a configurable directory on disk and is read
//     back through a controlled HTTP route (see LocalStorage.ServeHTTP), which
//     maps a request path to a key, blocks path traversal, and sets the right
//     Content-Type. This is the default for dev/self-hosted deployments.
//   - S3Storage is a skeleton for an object-store backend (AWS S3 or
//     compatible). The interface is wired up; the implementation is left as a
//     stub so the credentials/SDK can be dropped in without touching callers.
package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by Open when a key does not exist.
var ErrNotFound = errors.New("storage: object not found")

// ErrNotImplemented is returned by backends that do not support an operation
// (e.g. the S3 skeleton, or Open on a backend that serves via its own URL).
var ErrNotImplemented = errors.New("storage: not implemented")

// ErrUnsupportedImage is returned by DetectImageType for non-image or
// unsupported-image payloads.
var ErrUnsupportedImage = errors.New("storage: unsupported image type")

// ObjectStorage stores binary blobs behind a pluggable backend.
//
// key is a backend-relative path such as "avatars/80000001/ab12cd.png". Callers
// own the key scheme; backends must treat it as an opaque, slash-separated path
// and must reject attempts to escape their root.
type ObjectStorage interface {
	// Put stores data under key with the given contentType and returns the
	// public URL at which the object can be retrieved.
	Put(ctx context.Context, key, contentType string, data []byte) (url string, err error)

	// Open returns a reader for a previously stored object plus its content
	// type. Backends that serve objects via their own URL (e.g. S3) may return
	// ErrNotImplemented. Returns ErrNotFound when the key is unknown.
	Open(ctx context.Context, key string) (body io.ReadCloser, contentType string, err error)
}

// ImageType describes a sniffed, supported image format.
type ImageType struct {
	Ext  string // file extension without the dot, e.g. "jpg"
	MIME string // canonical MIME type, e.g. "image/jpeg"
}

// Supported image signatures, sniffed from the leading bytes of the payload.
// Only the formats the product accepts for avatars/KYC are listed; extend here
// to support more.
var (
	imgJPEG = ImageType{Ext: "jpg", MIME: "image/jpeg"}
	imgPNG  = ImageType{Ext: "png", MIME: "image/png"}
	imgWEBP = ImageType{Ext: "webp", MIME: "image/webp"}
)

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// DetectImageType sniffs data's magic bytes and returns the matching supported
// image type. It never trusts a client-supplied Content-Type. Supported:
// JPEG, PNG, WEBP. Returns ErrUnsupportedImage otherwise.
func DetectImageType(data []byte) (ImageType, error) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return imgJPEG, nil
	case len(data) >= len(pngMagic) && bytes.Equal(data[:len(pngMagic)], pngMagic):
		return imgPNG, nil
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return imgWEBP, nil
	default:
		return ImageType{}, ErrUnsupportedImage
	}
}

// mimeByExt maps a stored key's extension back to its canonical MIME type. Used
// when serving files whose type must match how they were stored.
func mimeByExt(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return imgJPEG.MIME
	case "png":
		return imgPNG.MIME
	case "webp":
		return imgWEBP.MIME
	default:
		return "application/octet-stream"
	}
}
