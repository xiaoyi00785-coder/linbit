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
	"github.com/shopspring/decimal"
)

const dp = 8

func floor8(d decimal.Decimal) decimal.Decimal { return d.RoundFloor(dp) }

type Server struct {
	cfg    Config
	db     *sql.DB
	mu     sync.Mutex   // serialises balance-moving + listing/offer state changes
	offset atomic.Int64 // dev clock offset (ns)
}

func NewServer(cfg Config, db *sql.DB) *Server { return &Server{cfg: cfg, db: db} }

func (s *Server) now() time.Time { return time.Now().Add(time.Duration(s.offset.Load())) }

type userErr struct {
	Status int
	Code   string
	Msg    string
}

func (e *userErr) Error() string { return e.Code + ": " + e.Msg }

func bad(code, msg string) error      { return &userErr{Status: http.StatusBadRequest, Code: code, Msg: msg} }
func notFound(code, msg string) error { return &userErr{Status: http.StatusNotFound, Code: code, Msg: msg} }
func conflict(code, msg string) error { return &userErr{Status: http.StatusConflict, Code: code, Msg: msg} }
func forbidden(code, msg string) error {
	return &userErr{Status: http.StatusForbidden, Code: code, Msg: msg}
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
	log.Printf("nft: %v", err)
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

func (s *Server) optUser(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { next(w, r, r.Header.Get("X-User-Id")) }
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
	s.collectionRoutes(mux)
	s.itemRoutes(mux)
	s.listingRoutes(mux)
	s.offerRoutes(mux)
	s.activityRoutes(mux)
	s.adminRoutes(mux)
	// Dev: move service clock forward and run one expiry tick (tests only).
	mux.HandleFunc("POST /internal/dev/nft/advance", s.dev(func(w http.ResponseWriter, r *http.Request) {
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
	t := time.NewTicker(s.cfg.ExpiryEvery)
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

func (s *Server) Tick(ctx context.Context) {
	if err := s.expireListings(ctx); err != nil {
		log.Printf("nft expireListings: %v", err)
	}
	if err := s.expireOffers(ctx); err != nil {
		log.Printf("nft expireOffers: %v", err)
	}
	if err := s.recomputeVolume(ctx); err != nil {
		log.Printf("nft recomputeVolume: %v", err)
	}
}
