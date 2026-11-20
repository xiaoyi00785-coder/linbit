package main

import (
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// server.go: HTTP routing. Public market data is anonymous; user actions want
// an X-User-Id header (injected by the gateway after JWT auth).

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// public market data
	mux.HandleFunc("GET /options/instruments", s.hInstruments)
	mux.HandleFunc("GET /options/chain", s.hChain)
	mux.HandleFunc("GET /options/ticker/{instrument}", s.hTicker)

	// user actions (gateway injects X-User-Id)
	mux.HandleFunc("POST /options/orders", s.user(s.hPlace))
	mux.HandleFunc("GET /options/orders", s.user(s.hMyOrders))
	mux.HandleFunc("DELETE /options/orders/{id}", s.user(s.hCancel))
	mux.HandleFunc("GET /options/positions", s.user(s.hPositions))
	mux.HandleFunc("GET /options/trades", s.user(s.hTrades))
	return mux
}

type userHandler func(w http.ResponseWriter, r *http.Request, uid string)

func (s *Server) user(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-Id")
		if uid == "" || strings.HasPrefix(uid, "system:") {
			writeError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		h(w, r, uid)
	}
}

// writeJSON / writeError delegate to the common package.
func writeJSON(w http.ResponseWriter, status int, v any) { common.WriteJSON(w, status, v) }
func writeError(w http.ResponseWriter, status int, code, msg string) {
	common.WriteError(w, status, code, msg)
}
