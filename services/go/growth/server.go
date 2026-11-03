package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

type Server struct {
	cfg Config
	db  *sql.DB
	mu  sync.Mutex // serializes sync / settlement / reward issuance
	now func() time.Time
}

func NewServer(cfg Config, db *sql.DB) *Server {
	return &Server{cfg: cfg, db: db, now: time.Now}
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
	log.Printf("growth: %v", err)
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
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	s.partnerRoutes(mux)
	s.referralRoutes(mux)
	s.extraRoutes(mux)
	mux.HandleFunc("POST /internal/dev/growth/sync", s.dev(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Sync(r.Context()); err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	// Dev: settle everything up to now (or ?cutoff=RFC3339) and pay out.
	mux.HandleFunc("POST /internal/dev/growth/settle", s.dev(func(w http.ResponseWriter, r *http.Request) {
		cutoff := s.now()
		if v := r.URL.Query().Get("cutoff"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				cutoff = t
			}
		}
		if err := s.Sync(r.Context()); err != nil {
			writeErr(w, err)
			return
		}
		id, err := s.Settle(r.Context(), cutoff)
		if err != nil {
			writeErr(w, err)
			return
		}
		paid, err := s.PayPending(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"settlementId": id, "paid": paid})
	}))
	return mux
}

// Run drives feed sync, the daily settlement and payout retries.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.SyncEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.Sync(ctx); err != nil {
			log.Printf("growth sync: %v", err)
		}
		if cut := s.lastBoundary(s.now()); !cut.IsZero() {
			var exists bool
			_ = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM g_settlements WHERE cutoff >= $1)`, cut).Scan(&exists)
			if !exists {
				if _, err := s.Settle(ctx, cut); err != nil {
					log.Printf("growth settle: %v", err)
				}
			}
		}
		if _, err := s.PayPending(ctx); err != nil {
			log.Printf("growth payout: %v", err)
		}
		s.Housekeeping(ctx)
	}
}

// lastBoundary is the most recent daily settlement instant <= now.
func (s *Server) lastBoundary(now time.Time) time.Time {
	now = now.UTC()
	b := time.Date(now.Year(), now.Month(), now.Day(), s.cfg.SettleHourUTC, 0, 0, 0, time.UTC)
	if b.After(now) {
		b = b.Add(-24 * time.Hour)
	}
	return b
}

// day returns the business date (UTC+8 by default) of t.
func (s *Server) day(t time.Time) string { return t.UTC().Add(s.cfg.DayOffset).Format("2006-01-02") }

func pageArgs(r *http.Request) (page, size int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	size, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return
}

func trunc(d decimal.Decimal, places int32) decimal.Decimal { return d.Truncate(places) }
