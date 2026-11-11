// Package storage 是各 Go 服务共用的对象存储。头像在用；KYC 文件复用同一接口，换后端或 key 前缀即可。
//
// ObjectStorage 按调用方给的 key 存 blob，并返回可取回的 URL。两个后端：
//
//   - LocalStorage 写到本地目录，经受控 HTTP（LocalStorage.ServeHTTP）读回：
//     路径映射到 key，挡住 ".."，并按扩展名设 Content-Type。开发/自建默认用它。
//   - S3Storage 走 S3 兼容存储（AWS / MinIO / R2），path-style + SigV4，不依赖 AWS SDK。
package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
)

// ErrNotFound 表示 Open 时 key 不存在。
var ErrNotFound = errors.New("storage: object not found")

// ErrNotImplemented 表示该后端不做这个操作。S3 的 Open 已实现；接口仍保留这个错误给只靠自身 URL 对外的后端。
var ErrNotImplemented = errors.New("storage: not implemented")

// ErrUnsupportedImage 表示 DetectImageType 见到的不是支持的图片。只认 JPEG、PNG、WEBP。
var ErrUnsupportedImage = errors.New("storage: unsupported image type")

// ObjectStorage 把二进制对象放到可替换的后端。
// key 是后端相对路径，例如 avatars/80000001/ab12cd.png。调用方定 key 规则；
// 后端把它当成不透明的斜杠路径，并拒绝逃出根目录。
type ObjectStorage interface {
	// Put stores data under key with the given contentType and returns the
	// public URL at which the object can be retrieved.
	Put(ctx context.Context, key, contentType string, data []byte) (url string, err error)

	// Open 返回已存对象的读取器和 Content-Type。key 不存在则 ErrNotFound。
	// 只靠自身 URL 对外的后端可以返回 ErrNotImplemented。当前 S3 实现会真正去取对象。
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

// DetectImageType 看魔数，不信客户端给的 Content-Type。只支持 JPEG、PNG、WEBP，其余 ErrUnsupportedImage。
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
