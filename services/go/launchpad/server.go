package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linbit/platform/services/go/common"
)

const dp = 8 // decimal precision for amounts

type Server struct {
	cfg    Config
	db     *sql.DB
	mu     sync.Mutex   // serialises every balance-moving operation
	offset atomic.Int64 // dev clock offset (ns)
}

func NewServer(cfg Config, db *sql.DB) *Server { return &Server{cfg: cfg, db: db} }

func (s *Server) now() time.Time { return time.Now().Add(time.Duration(s.offset.Load())) }

func (s *Server) hourBucket(t time.Time) int { return int(t.Unix() / 3600) }

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
func conflict(code, msg string) error {
	return &userErr{Status: http.StatusConflict, Code: code, Msg: msg}
}

func writeErr(w http.ResponseWriter, err error) {
	var ue *userErr
	if errors.As(err, &ue) {
		common.WriteError(w, ue.Status, ue.Code, ue.Msg)
		return
	}
	var ae *apiErr
	if errors.As(err, &ae) && ae.Status/100 == 4 && ae.Code != "" {
		common.WriteError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Printf("launchpad: %v", err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func (s *Server) user(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-Id")
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

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	// Public catalogue (anonymous reads mirrored in the gateway allow-list).
	mux.HandleFunc("GET /launchpad/projects", s.handleProjects)
	mux.HandleFunc("GET /launchpad/projects/{id}", s.handleProject)
	mux.HandleFunc("GET /launchpad/projects/{id}/rules", s.handleRules)

	// Authenticated endpoints.
	mux.HandleFunc("POST /launchpad/projects/{id}/subscribe", s.user(s.handleSubscribe))
	mux.HandleFunc("POST /launchpad/projects/{id}/cancel", s.user(s.handleCancel))
	mux.HandleFunc("GET /launchpad/subscriptions", s.user(s.handleUserSubscriptions))
	mux.HandleFunc("GET /launchpad/subscriptions/{projectId}", s.user(s.handleUserSubscription))

	// Admin management.
	mux.HandleFunc("POST /internal/launchpad/projects", s.internal(s.handleUpsertProject))
	mux.HandleFunc("GET /internal/launchpad/projects", s.internal(s.handleAdminProjects))
	mux.HandleFunc("POST /internal/launchpad/projects/{id}/settle", s.internal(s.handleForceSettle))
	mux.HandleFunc("POST /internal/launchpad/projects/{id}/distribute", s.internal(s.handleForceDistribute))
	mux.HandleFunc("POST /internal/launchpad/projects/{id}/cancel", s.internal(s.handleAdminCancel))

	// Dev: advance the service clock and run one tick.
	mux.HandleFunc("POST /internal/dev/launchpad/advance", s.dev(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hours float64 `json:"hours"`
		}
		_ = common.DecodeJSON(r, &req)
		s.offset.Add(int64(req.Hours * float64(time.Hour)))
		s.Tick(r.Context())
		common.WriteJSON(w, http.StatusOK, map[string]any{"now": s.now().UnixMilli()})
	}))
	return mux
}

func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.TickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.Tick(ctx)
	}
}

// Tick opens due windows, snapshots every live subscription, settles and
// distributes projects whose scheduled times have passed.
func (s *Server) Tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.openWindows(ctx); err != nil {
		log.Printf("launchpad: open windows: %v", err)
	}
	if err := s.takeSnapshots(ctx); err != nil {
		log.Printf("launchpad: snapshots: %v", err)
	}
	if err := s.settleDue(ctx); err != nil {
		log.Printf("launchpad: settle: %v", err)
	}
	if err := s.distributeDue(ctx); err != nil {
		log.Printf("launchpad: distribute: %v", err)
	}
}
