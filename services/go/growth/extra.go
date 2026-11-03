package main

import (
	"context"
	"net/http"
)

// Hooks for later growth programs (points, gifts, activities, coupons).
func (s *Server) extraRoutes(mux *http.ServeMux)            {}
func (s *Server) afterSyncLocked(ctx context.Context) error { return nil }
func (s *Server) Housekeeping(ctx context.Context)          {}
