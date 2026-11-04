package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Server wires configuration, storage and HTTP handlers for the asset service.
type Server struct {
	// hedgeBlocked != "" disables convert hedging (mainnet: no valid hedge
	// account). Convert itself keeps working.
	hedgeBlocked string
	cfg    Config
	store  *Store
	prices PriceSource
	now    func() time.Time

	depeg     depegGuard
	hedge     *Hedger
	hedgeOnce sync.Once
}

// NewServer constructs a Server.
func NewServer(cfg Config, store *Store) *Server {
	return &Server{cfg: cfg, store: store, prices: marketIndexSource(cfg.MarketURL), now: time.Now}
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
	mux.HandleFunc("GET /assets/overview", s.handleOverview)

	// Wallet user-facing endpoints (/wallet/*) live in the wallet service; this
	// service exposes only the internal double-entry operations it drives.

	mux.HandleFunc("POST /internal/accounts", s.internal(s.handleInitAccount))
	mux.HandleFunc("POST /internal/credit", s.internal(s.handleCredit))
	mux.HandleFunc("POST /internal/freeze", s.internal(s.handleFreezeAcct))
	mux.HandleFunc("POST /internal/unfreeze", s.internal(s.handleUnfreezeAcct))
	mux.HandleFunc("POST /internal/account-transfer", s.internal(s.handleAccountTransfer))
	mux.HandleFunc("POST /internal/user-transfer", s.internal(s.handleUserTransfer))
	mux.HandleFunc("GET /internal/admin/asset-settings", s.internal(s.handleGetSettings))
	mux.HandleFunc("PUT /internal/admin/asset-settings", s.internal(s.handlePutSettings))
	mux.HandleFunc("POST /internal/deposit", s.internal(s.handleDeposit))
	mux.HandleFunc("POST /internal/withdraw-complete", s.internal(s.handleWithdrawComplete))
	mux.HandleFunc("POST /internal/settle", s.internal(s.handleSettle))
	mux.HandleFunc("GET /internal/balances", s.internal(s.handleInternalBalances))
	mux.HandleFunc("GET /internal/admin/ledger", s.internal(s.handleAdminLedger))
	mux.HandleFunc("POST /internal/reward", s.internal(s.handleReward))
	mux.HandleFunc("POST /internal/futures/transfer-out", s.internal(s.handleFuturesTransfer(true)))
	mux.HandleFunc("POST /internal/futures/transfer-in", s.internal(s.handleFuturesTransfer(false)))
	mux.HandleFunc("POST /internal/system-transfer", s.internal(s.handleSystemTransfer))
	mux.HandleFunc("GET /internal/ops/feed", s.internal(s.handleOpsFeed))
	mux.HandleFunc("POST /internal/mm/testnet-seed", s.internal(s.handleMMSeed))
	mux.HandleFunc("POST /internal/admin/user-credit", s.internal(s.handleUserCredit))
	mux.HandleFunc("POST /internal/admin/adjust", s.internal(s.handleAdminAdjust))
	mux.HandleFunc("GET /internal/solvency/liabilities", s.internal(s.handleLiabilities))
	s.convertRoutes(mux)
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
	// PriceUsd is the live market-data index price (USDT) used for the valuation;
	// clients must show it as the unit price rather than valuationUsd/quantity,
	// which is distorted for dust balances by the 2-decimal valuation rounding.
	PriceUsd *string `json:"priceUsd,omitempty"`
}

type paginatedBalances struct {
	Items    []balanceDTO `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Total    int          `json:"total"`
	Account  string       `json:"account"` // which wallet was listed: spot | funding
}

// balancesAccount normalises ?account= (case/space-insensitive; "" = spot).
func balancesAccount(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", AcctSpot:
		return AcctSpot, true
	case AcctFunding:
		return AcctFunding, true
	}
	return "", false
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

	acct, ok := balancesAccount(r.URL.Query().Get("account"))
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "account 必须为 spot 或 funding")
		return
	}
	if acct == AcctFunding {
		all, err := s.store.WalletBalances(r.Context(), AcctFunding, userID)
		if err != nil {
			log.Printf("balances: funding: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items := []balanceDTO{}
		for i := (page - 1) * pageSize; i < len(all) && i < page*pageSize; i++ {
			items = append(items, toBalanceDTO(all[i]))
		}
		common.WriteJSON(w, http.StatusOK, paginatedBalances{Items: items, Page: page, PageSize: pageSize, Total: len(all), Account: AcctFunding})
		return
	}

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
		Account:  AcctSpot,
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

type walletMovementRequest struct {
	UserID     string `json:"userId"`
	Asset      string `json:"asset"`
	Amount     string `json:"amount"`
	Gas        string `json:"gas"`
	FeeAccount string `json:"feeAccount"`
	Ref        string `json:"ref"`
	Account    string `json:"account"` // deposit only: spot|funding (default: deposit_account setting)
}

// handleDeposit credits a confirmed on-chain deposit (idempotent on ref).
func (s *Server) handleDeposit(w http.ResponseWriter, r *http.Request) {
	var req walletMovementRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	acct := req.Account
	if acct == "" {
		acct = s.store.depositAccount(r.Context())
	}
	if !validAcct(acct) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "account 不合法")
		return
	}
	err = s.store.DepositTo(r.Context(), acct, req.UserID, req.Asset, amount, req.Ref)
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case err != nil:
		log.Printf("deposit: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleWithdrawComplete debits a confirmed withdrawal from locked and books the
// gas to the platform account (idempotent on ref).
func (s *Server) handleWithdrawComplete(w http.ResponseWriter, r *http.Request) {
	var req walletMovementRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err1 := decimal.NewFromString(req.Amount)
	gas := decimal.Zero
	var err2 error
	if req.Gas != "" {
		gas, err2 = decimal.NewFromString(req.Gas)
	}
	if err1 != nil || err2 != nil || amount.Sign() <= 0 || gas.Sign() < 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	err := s.store.WithdrawComplete(r.Context(), req.UserID, req.Asset, amount, gas, req.FeeAccount, req.Ref)
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
	case err != nil:
		log.Printf("withdraw-complete: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
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

// settleRequest is the internal settlement payload sent by the order service.
type settleRequest struct {
	TradeID        string `json:"tradeId"`
	Base           string `json:"base"`
	Quote          string `json:"quote"`
	BuyerUserID    string `json:"buyerUserId"`
	SellerUserID   string `json:"sellerUserId"`
	FeeAccount     string `json:"feeAccount"`
	QuoteAmount    string `json:"quoteAmount"`
	BaseQty        string `json:"baseQty"`
	BuyerFeeBase   string `json:"buyerFeeBase"`
	SellerFeeQuote string `json:"sellerFeeQuote"`
}

func (s *Server) handleSettle(w http.ResponseWriter, r *http.Request) {
	var req settleRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.TradeID == "" || req.Base == "" || req.Quote == "" ||
		req.BuyerUserID == "" || req.SellerUserID == "" || req.FeeAccount == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	dec := func(s string) decimal.Decimal {
		if s == "" {
			return decimal.Zero
		}
		return decimal.RequireFromString(s)
	}
	quoteAmount, err1 := decimal.NewFromString(req.QuoteAmount)
	baseQty, err2 := decimal.NewFromString(req.BaseQty)
	if err1 != nil || err2 != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	sr := SettleRequest{
		TradeID:        req.TradeID,
		Base:           req.Base,
		Quote:          req.Quote,
		BuyerUserID:    req.BuyerUserID,
		SellerUserID:   req.SellerUserID,
		FeeAccount:     req.FeeAccount,
		QuoteAmount:    quoteAmount,
		BaseQty:        baseQty,
		BuyerFeeBase:   dec(req.BuyerFeeBase),
		SellerFeeQuote: dec(req.SellerFeeQuote),
	}
	err := s.store.Settle(r.Context(), sr)
	switch {
	case errors.Is(err, ErrAlreadySettled):
		// Idempotent: already applied, report success without re-applying.
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_settled"})
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
	case errors.Is(err, ErrInvalidAmount):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
	case err != nil:
		log.Printf("settle: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "settled"})
	}
}

func (s *Server) handleInternalBalances(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 userId")
		return
	}
	if err := s.store.EnsureAccount(r.Context(), userID); err != nil {
		log.Printf("internal balances: ensure: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	var balances []Balance
	var total int
	var err error
	if r.URL.Query().Get("account") == AcctFunding {
		balances, err = s.store.WalletBalances(r.Context(), AcctFunding, userID)
		total = len(balances)
	} else {
		balances, total, err = s.store.Balances(r.Context(), userID, 1, 1000)
	}
	if err != nil {
		log.Printf("internal balances: %v", err)
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
	common.WriteJSON(w, http.StatusOK, paginatedBalances{Items: items, Page: 1, PageSize: 1000, Total: total})
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
