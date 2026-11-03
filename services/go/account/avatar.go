package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/common/storage"
)

// avatarFormField is the multipart field name carrying the image file.
const avatarFormField = "file"

// handleUploadAvatar accepts a multipart/form-data image upload, validates its
// size and (content-sniffed) type, stores it via the pluggable ObjectStorage,
// and persists the resulting URL on the account.
//
//	POST /account/avatar   (multipart/form-data, field "file")
func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if s.avatars == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "头像存储不可用")
		return
	}

	// Cap the whole request body so an oversized upload is rejected without
	// buffering it all into memory.
	maxBytes := s.cfg.AvatarMaxBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024) // small slack for multipart envelope
	if err := r.ParseMultipartForm(maxBytes + 1024); err != nil {
		common.WriteError(w, http.StatusBadRequest, "FILE_TOO_LARGE", "文件过大或表单无效")
		return
	}
	file, _, err := r.FormFile(avatarFormField)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少上传文件字段 file")
		return
	}
	defer file.Close()

	// Read up to maxBytes+1; anything beyond the limit is rejected.
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "读取文件失败")
		return
	}
	if int64(len(data)) > maxBytes {
		common.WriteError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "头像超过大小限制")
		return
	}
	if len(data) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "文件为空")
		return
	}

	// Sniff the real image type; never trust the client-declared Content-Type.
	it, err := storage.DetectImageType(data)
	if err != nil {
		common.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "仅支持 JPEG/PNG/WEBP 图片")
		return
	}

	// Key is relative to the storage root (the avatars dir / bucket prefix); the
	// backend maps it to publicBase + "/" + key for retrieval.
	key := u.UID + "/" + randomName() + "." + it.Ext
	url, err := s.avatars.Put(r.Context(), key, it.MIME, data)
	if err != nil {
		if errors.Is(err, storage.ErrNotImplemented) {
			common.WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "当前存储后端暂不支持上传")
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}

	if err := s.store.UpdateProfile(r.Context(), u.ID, nil, &url); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	u, _ = s.store.UserByUID(r.Context(), u.UID)
	common.WriteJSON(w, http.StatusOK, toAccountResponse(u))
}

// handleServeAvatar streams a stored avatar through the controlled route. Only
// the local backend serves bytes here; other backends (e.g. S3) return their own
// URLs and this route 404s.
func (s *Server) handleServeAvatar(w http.ResponseWriter, r *http.Request) {
	local, ok := s.avatars.(*storage.LocalStorage)
	if !ok {
		http.NotFound(w, r)
		return
	}
	local.ServeHTTP(w, r, s.cfg.AvatarPublicBase)
}

// randomName returns a short random hex string for an avatar filename.
func randomName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "avatar"
	}
	return hex.EncodeToString(b)
}
