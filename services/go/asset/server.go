package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Server wires configuration, storage and HTTP handlers for the asset service.
type Server struct {
	cfg   Config
	store *Store
}

// NewServer constructs a Server.
func NewServer(cfg Config, store *Store) *Server {
	return &Server{cfg: cfg, store: store}
}

// Routes returns the asset service HTTP handler.
//
// /assets/* are user-facing and expect the X-User-Id header injected by the
// gateway after it validates the JWT. /internal/* are service-to-service and
// require the shared X-Internal-Token; the gateway never proxies them.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	mux.HandleFunc("GET /assets/balances", s.handleBalances)

	mux.HandleFunc("POST /internal/accounts", s.internal(s.handleInitAccount))
	mux.HandleFunc("POST /internal/credit", s.internal(s.handleCredit))
	mux.HandleFunc("POST /internal/freeze", s.internal(s.handleFreeze))
	mux.HandleFunc("POST /internal/unfreeze", s.internal(s.handleUnfreeze))
	return mux
}

// internal guards service-to-service endpoints with the shared token.
func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

// ---- DTOs ----

type balanceDTO struct {
	Asset        string  `json:"asset"`
	Free         string  `json:"free"`
	Locked       string  `json:"locked"`
	Total        string  `json:"total"`
	ValuationUsd *string `json:"valuationUsd"`
}

type paginatedBalances struct {
	Items    []balanceDTO `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Total    int          `json:"total"`
}

type accountRequest struct {
	UserID string `json:"userId"`
}

type movementRequest struct {
	UserID string `json:"userId"`
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
	Ref    string `json:"ref"`
}

// ---- Handlers ----

func (s *Server) handleBalances(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	// Lazily ensure the account exists so a freshly-registered user always has
	// balances even if the register-time init call did not reach this service.
	if err := s.store.EnsureAccount(r.Context(), userID); err != nil {
		log.Printf("balances: ensure account: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}

	page := atoiDefault(r.URL.Query().Get("page"), 1, 1, 1<<30)
	pageSize := atoiDefault(r.URL.Query().Get("pageSize"), 20, 1, 100)

	balances, total, err := s.store.Balances(r.Context(), userID, page, pageSize)
	if err != nil {
		log.Printf("balances: query: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]balanceDTO, 0, len(balances))
	for _, b := range balances {
		items = append(items, balanceDTO{
			Asset:  b.Asset,
			Free:   b.Free.StringFixed(8),
			Locked: b.Locked.StringFixed(8),
			Total:  b.Total().StringFixed(8),
		})
	}
	common.WriteJSON(w, http.StatusOK, paginatedBalances{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	})
}

func (s *Server) handleInitAccount(w http.ResponseWriter, r *http.Request) {
	var req accountRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if err := s.store.EnsureAccount(r.Context(), req.UserID); err != nil {
		log.Printf("init account: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"userId": req.UserID, "status": "ok"})
}

func (s *Server) handleCredit(w http.ResponseWriter, r *http.Request) {
	s.handleMovement(w, r, s.store.Credit)
}

func (s *Server) handleFreeze(w http.ResponseWriter, r *http.Request) {
	s.handleMovement(w, r, s.store.Freeze)
}

func (s *Server) handleUnfreeze(w http.ResponseWriter, r *http.Request) {
	s.handleMovement(w, r, s.store.Unfreeze)
}

type movementFn func(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error

func (s *Server) handleMovement(w http.ResponseWriter, r *http.Request, fn movementFn) {
	var req movementRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	err = fn(r.Context(), req.UserID, req.Asset, amount, req.Ref)
	switch {
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
		return
	case err != nil:
		log.Printf("movement: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func atoiDefault(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}
