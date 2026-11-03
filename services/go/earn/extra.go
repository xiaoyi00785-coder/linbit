package main

import (
	"context"
	"net/http"
)

// Hooks for the savings products (added separately).
func (s *Server) extraRoutes(mux *http.ServeMux)      {}
func (s *Server) extraTick(ctx context.Context) error { return nil }
