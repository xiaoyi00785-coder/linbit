package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// Server wires the store, WebSocket hub and HTTP surface for chat-svc.
type Server struct {
	cfg   Config
	store *Store
	hub   *Hub
	chat  *Chat
	cs    *CS
}

// NewServer constructs a Server and wires routes-owning subsystems.
func NewServer(cfg Config, store *Store) *Server {
	return &Server{cfg: cfg, store: store, hub: NewHub()}
}

// Routes returns the chat-svc HTTP handler. /chat/* and /cs/* are forwarded by
// the gateway (which injects X-User-Id for authed paths and allow-lists the
// public reads and /cs/guest/*); /ws is the WebSocket hub; /internal/* is the
// admin surface guarded by X-Internal-Token.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	s.chatRoutes(mux)
	s.csRoutes(mux)
	// WS hub: /ws plus per-surface aliases the gateway exposes publicly
	// (/chat/ws, /cs/ws) so chat-svc live channels do not collide with
	// market-data's public /ws.
	mux.HandleFunc("GET /ws", s.hub.ServeHTTP)
	mux.HandleFunc("GET /chat/ws", s.hub.ServeHTTP)
	mux.HandleFunc("GET /cs/ws", s.hub.ServeHTTP)
	return mux
}

// internal guards admin endpoints with the shared internal token.
func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := s.cfg.InternalToken
		if (tok == "" && !s.cfg.DevMode) || (tok != "" && r.Header.Get("X-Internal-Token") != tok) {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}
