package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

type Server struct {
	cfg     Config
	db      *sql.DB
	http    *http.Client
	mu      sync.Mutex // serialises operations that mutate a given user's wallet
	userLks userLocks
}

func NewServer(cfg Config, db *sql.DB) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		http:    &http.Client{Timeout: 20 * time.Second},
		userLks: newUserLocks(),
	}
}

// userLocks gives each user their own mutex so one user's long-running RPC
// call doesn't block another user's wallet operation.
type userLocks struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func newUserLocks() userLocks { return userLocks{m: map[string]*sync.Mutex{}} }
func (ul *userLocks) lock(uid string) *sync.Mutex {
	ul.mu.Lock()
	defer ul.mu.Unlock()
	m, ok := ul.m[uid]
	if !ok {
		m = &sync.Mutex{}
		ul.m[uid] = m
	}
	return m
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
func unauth(code, msg string) error {
	return &userErr{Status: http.StatusUnauthorized, Code: code, Msg: msg}
}
func locked(code, msg string) error {
	return &userErr{Status: http.StatusLocked, Code: code, Msg: msg}
}
func unavailable(code, msg string) error {
	return &userErr{Status: http.StatusServiceUnavailable, Code: code, Msg: msg}
}

func writeErr(w http.ResponseWriter, err error) {
	var ue *userErr
	if errors.As(err, &ue) {
		common.WriteError(w, ue.Status, ue.Code, ue.Msg)
		return
	}
	log.Printf("web3wallet: %v", err)
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

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	// Wallet lifecycle.
	mux.HandleFunc("POST /web3/wallets", s.user(s.handleCreateWallet))
	mux.HandleFunc("GET /web3/wallets", s.user(s.handleGetWallet))
	mux.HandleFunc("POST /web3/wallets/change-pin", s.user(s.handleChangePIN))
	mux.HandleFunc("POST /web3/wallets/reset", s.user(s.handleResetWallet))
	mux.HandleFunc("POST /web3/wallets/export", s.user(s.handleExportMnemonic))
	mux.HandleFunc("POST /web3/wallets/backup-ack", s.user(s.handleBackupAck))

	// Addresses + balances + tokens + receive.
	mux.HandleFunc("GET /web3/wallets/addresses", s.user(s.handleAddresses))
	mux.HandleFunc("GET /web3/wallets/balances", s.user(s.handleBalances))
	mux.HandleFunc("GET /web3/wallets/receive", s.user(s.handleReceive))
	mux.HandleFunc("GET /web3/tokens/search", s.user(s.handleTokenSearch))
	mux.HandleFunc("GET /web3/wallets/tokens", s.user(s.handleListCustomTokens))
	mux.HandleFunc("POST /web3/wallets/tokens", s.user(s.handleAddCustomToken))
	mux.HandleFunc("DELETE /web3/wallets/tokens/{id}", s.user(s.handleRemoveCustomToken))

	// Transactions.
	mux.HandleFunc("POST /web3/transactions", s.user(s.handleSend))
	mux.HandleFunc("GET /web3/wallets/history", s.user(s.handleHistory))
	mux.HandleFunc("GET /web3/transactions/{hash}", s.user(s.handleTxStatus))
	mux.HandleFunc("GET /web3/gas", s.user(s.handleGas))

	// NFTs.
	mux.HandleFunc("GET /web3/wallets/nfts", s.user(s.handleNFTs))

	// Swap + bridge (quote is public-ish; execute signs).
	mux.HandleFunc("POST /web3/swap/quote", s.user(s.handleSwapQuote))
	mux.HandleFunc("POST /web3/swap/execute", s.user(s.handleSwapExecute))
	mux.HandleFunc("POST /web3/bridge/quote", s.user(s.handleBridgeQuote))
	mux.HandleFunc("POST /web3/bridge/execute", s.user(s.handleBridgeExecute))

	// WalletConnect v2 — the server keeps session metadata and signs on
	// behalf of the client; relay transport runs on the client.
	mux.HandleFunc("GET /web3/walletconnect/config", s.user(s.handleWCConfig))
	mux.HandleFunc("POST /web3/walletconnect/sessions", s.user(s.handleWCPair))
	mux.HandleFunc("GET /web3/walletconnect/sessions", s.user(s.handleWCList))
	mux.HandleFunc("DELETE /web3/walletconnect/sessions/{topic}", s.user(s.handleWCDisconnect))
	mux.HandleFunc("POST /web3/signatures", s.user(s.handleSign))

	// Admin / observability.
	mux.HandleFunc("GET /internal/web3/stats", s.internal(s.handleAdminStats))

	return mux
}

// runBackground re-checks the status of every still-pending transaction every
// 30s. A tx is bumped to confirmed/failed/dropped based on receipt lookups; a
// tx last-seen >24h ago with no receipt is flipped to dropped.
func (s *Server) runBackground(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.sweepPendingTxs(ctx)
	}
}
