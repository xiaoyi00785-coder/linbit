package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// handleAdminStats gives ops a cheap read of the counters they need for the
// Grafana / ops-dashboard tiles — wallets, pending tx backlog, pin lockouts.
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var wallets, locked, pending, dropped, wcActive int64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web3_wallets`).Scan(&wallets)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web3_wallets WHERE locked_until > NOW()`).Scan(&locked)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web3_transactions WHERE status='pending'`).Scan(&pending)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web3_transactions WHERE status='dropped'`).Scan(&dropped)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web3_wc_sessions WHERE status='active'`).Scan(&wcActive)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"wallets":             wallets,
		"pinLocked":           locked,
		"pendingTxs":          pending,
		"droppedTxs":          dropped,
		"walletConnectActive": wcActive,
	})
}
