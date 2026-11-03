package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/common/storage"
)

// docFormField is the multipart field name carrying the document file.
const docFormField = "file"

// handleUploadDocument accepts a multipart/form-data document upload (field
// "file" plus side=front|selfie|back), validates its size and sniffed type,
// stores it via the pluggable ObjectStorage, and returns an opaque fileId the
// user later references when submitting an application.
//
//	POST /kyc/documents   (multipart/form-data: file, side)
func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	if s.docs == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "证件存储不可用")
		return
	}

	maxBytes := s.cfg.DocMaxBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+4096) // slack for multipart envelope
	if err := r.ParseMultipartForm(maxBytes + 4096); err != nil {
		common.WriteError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件过大或表单无效")
		return
	}
	side := strings.ToLower(strings.TrimSpace(r.FormValue("side")))
	if !validSide(side) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "side 必须为 front|selfie|back")
		return
	}
	file, _, err := r.FormFile(docFormField)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少上传文件字段 file")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "读取文件失败")
		return
	}
	if int64(len(data)) > maxBytes {
		common.WriteError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "证件文件超过大小限制")
		return
	}
	if len(data) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "文件为空")
		return
	}

	// Sniff the real image type; never trust the client Content-Type. The UI
	// accepts JPEG/PNG only (the default here); WEBP and others are rejected.
	it, err := storage.DetectImageType(data)
	if err != nil || (it.MIME != "image/jpeg" && it.MIME != "image/png") {
		common.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "仅支持 JPEG/PNG 图片")
		return
	}

	key := "kyc/" + uid + "/" + randomName() + "." + it.Ext
	if _, err := s.docs.Put(r.Context(), key, it.MIME, data); err != nil {
		if err == storage.ErrNotImplemented {
			common.WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "当前存储后端暂不支持上传")
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	id, err := s.store.InsertDocument(r.Context(), Document{
		UserID: uid, Side: side, FileKey: key, ContentType: it.MIME, SizeBytes: int64(len(data)),
	})
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"fileId": docFileID(id), "side": side})
}

// handleAdminDocument streams a stored document for controlled (internal-token)
// review. Documents are never publicly served.
//
//	GET /admin/kyc/documents/{id}
func (s *Server) handleAdminDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDocID(r.PathValue("id"))
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的文件编号")
		return
	}
	if s.docs == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "证件存储不可用")
		return
	}
	doc, err := s.store.GetDocument(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文件不存在")
		return
	}
	body, contentType, err := s.docs.Open(r.Context(), doc.FileKey)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "文件不可读取")
		return
	}
	defer body.Close()
	if contentType == "" {
		contentType = doc.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, body)
}

// validSide reports whether s is an accepted document side.
func validSide(s string) bool {
	return s == sideFront || s == sideSelfie || s == sideBack
}

// docFileID renders an opaque fileId from a document row id.
func docFileID(id int64) string { return "doc_" + strconv.FormatInt(id, 10) }

// parseDocID accepts either a raw numeric id or the "doc_<n>" form.
func parseDocID(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "doc_")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// randomName returns a short random hex string for a stored filename.
func randomName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "doc"
	}
	return hex.EncodeToString(b)
}
