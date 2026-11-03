package main

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/common/storage"
)

// Server wires configuration, storage, the document object store and the account
// client into the KYC HTTP handlers.
//
// /kyc/* are user-facing (the gateway injects X-User-Id after validating the JWT).
// /admin/* are review endpoints guarded by the shared X-Internal-Token and, like
// the wallet's admin review endpoints, are never proxied by the gateway.
type Server struct {
	cfg     Config
	store   *Store
	docs    storage.ObjectStorage
	account *AccountClient
}

// NewServer constructs a Server with the document store and account client wired in.
func NewServer(cfg Config, store *Store) *Server {
	return &Server{
		cfg:     cfg,
		store:   store,
		docs:    newDocStorage(cfg),
		account: NewAccountClient(cfg.AccountInternalURL, cfg.InternalToken),
	}
}

// newDocStorage builds the configured ObjectStorage backend for KYC documents.
// A local backend that fails to initialise leaves docs nil, and the upload
// handler reports the feature as unavailable.
func newDocStorage(cfg Config) storage.ObjectStorage {
	if strings.EqualFold(cfg.StorageBackend, "s3") {
		return storage.NewS3Storage(storage.S3Config{
			Bucket:     cfg.S3Bucket,
			Region:     cfg.S3Region,
			Endpoint:   cfg.S3Endpoint,
			PublicBase: cfg.S3PublicBase,
		})
	}
	ls, err := storage.NewLocalStorage(cfg.DocDir, cfg.DocPublicBase)
	if err != nil {
		log.Printf("kyc doc storage: init local backend: %v (upload disabled)", err)
		return nil
	}
	return ls
}

// Routes returns the KYC service HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// User-facing (routed via the gateway under /kyc/*).
	mux.HandleFunc("POST /kyc/documents", s.handleUploadDocument)
	mux.HandleFunc("POST /kyc", s.handleSubmit)
	mux.HandleFunc("GET /kyc/status", s.handleStatus)

	// Admin / review (internal token; never proxied by the gateway).
	mux.HandleFunc("GET /admin/kyc/applications", s.internal(s.handleAdminList))
	mux.HandleFunc("GET /admin/kyc/applications/{id}", s.internal(s.handleAdminGet))
	mux.HandleFunc("POST /admin/kyc/applications/{id}/approve", s.internal(s.handleAdminApprove))
	mux.HandleFunc("POST /admin/kyc/applications/{id}/reject", s.internal(s.handleAdminReject))
	mux.HandleFunc("GET /admin/kyc/documents/{id}", s.internal(s.handleAdminDocument))
	mux.HandleFunc("GET /admin/kyc/level-sync", s.internal(s.handleAdminLevelSync))
	mux.HandleFunc("GET /internal/kyc/country", s.internal(s.handleInternalCountry))
	return mux
}

// internal guards service-to-service / admin endpoints with the shared token.
func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

// requireUser extracts the gateway-injected user id.
func requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return "", false
	}
	return uid, true
}

// ---- shared DTOs ----

// statusDTO is the openapi KycStatus response.
type statusDTO struct {
	Level       int     `json:"level"`
	Status      string  `json:"status"`
	SubmittedAt *string `json:"submittedAt"`
	Reason      *string `json:"reason"`
}

func strptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// handleInternalCountry: GET /internal/kyc/country?userId= -> {userId, country, status}.
// Uses the latest application whatever its status (a declared country is a
// compliance signal even before approval). country "" = never submitted.
func (s *Server) handleInternalCountry(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.URL.Query().Get("userId"))
	if uid == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "userId 必填")
		return
	}
	a, err := s.store.LatestApplication(r.Context(), uid)
	if errors.Is(err, ErrNotFound) {
		common.WriteJSON(w, http.StatusOK, map[string]string{"userId": uid, "country": "", "status": "none"})
		return
	}
	if err != nil {
		log.Printf("kyc country: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"userId": uid, "country": strings.ToUpper(strings.TrimSpace(a.Country)), "status": a.Status})
}
