package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

type Server struct {
	cfg   Config
	db    *sql.DB
	store *Store
	hub   *eventHub

	mu      sync.Mutex
	workers int
}

func NewServer(cfg Config, db *sql.DB) *Server {
	return &Server{cfg: cfg, db: db, store: NewStore(db), hub: newEventHub()}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	// Authenticated user routes (X-User-Id set by gateway).
	mux.HandleFunc("POST /reports/exports", s.user(s.handleCreateExport))
	mux.HandleFunc("GET /reports/exports", s.user(s.handleListExports))
	mux.HandleFunc("GET /reports/exports/{jobId}", s.user(s.handleGetExport))
	mux.HandleFunc("GET /reports/exports/{jobId}/download", s.handleDownload) // token-authed
	mux.HandleFunc("POST /reports/tax", s.user(s.handleCreateTaxReport))

	mux.HandleFunc("GET /notifications/subscriptions", s.user(s.handleListSubscriptions))
	mux.HandleFunc("POST /notifications/subscriptions", s.user(s.handleCreateSubscription))
	mux.HandleFunc("DELETE /notifications/subscriptions/{id}", s.user(s.handleDeleteSubscription))

	// WebSocket activity stream. Token is in the ?token= query because browsers
	// cannot set Authorization on native WebSocket handshakes.
	mux.HandleFunc("GET /account/events", s.handleEventsWS)

	// Dev / internal push endpoint so other services can emit events to this
	// user's connected sockets (feature parity with the deposit watcher).
	mux.HandleFunc("POST /internal/events/push", s.internal(s.handleInternalEventPush))
	// Dev: run one tick of the deposit watcher synchronously.
	mux.HandleFunc("POST /internal/dev/reports/tick", s.dev(func(w http.ResponseWriter, r *http.Request) {
		s.depositTick(r.Context())
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	return mux
}

type userErr struct {
	Status int
	Code   string
	Msg    string
}

func (e *userErr) Error() string { return e.Code + ": " + e.Msg }

func bad(code, msg string) error {
	return &userErr{Status: http.StatusBadRequest, Code: code, Msg: msg}
}

func notFound(code, msg string) error {
	return &userErr{Status: http.StatusNotFound, Code: code, Msg: msg}
}

func writeErr(w http.ResponseWriter, err error) {
	var ue *userErr
	if errors.As(err, &ue) {
		common.WriteError(w, ue.Status, ue.Code, ue.Msg)
		return
	}
	log.Printf("reports: %v", err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

// user wraps a handler that needs the authenticated uid from X-User-Id (set
// by the gateway after it validated the access token).
func (s *Server) user(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := strings.TrimSpace(r.Header.Get("X-User-Id"))
		if uid == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		next(w, r, uid)
	}
}

func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

func (s *Server) dev(next http.HandlerFunc) http.HandlerFunc {
	return s.internal(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.DevMode {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在")
			return
		}
		next(w, r)
	})
}

// RunExpiryJanitor marks expired exports and removes their files. Runs hourly.
func (s *Server) RunExpiryJanitor(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		s.expireOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
