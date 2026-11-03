package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// solvency.go: futures-side liabilities for the daily solvency check. Funds
// moved into futures leave the spot balances table, so the futures wallets
// (fut_accounts.balance, every account incl. system ones) plus isolated
// margins are owed on top of spot balances. Unrealised PnL is zero-sum and
// not counted.

// GET /internal/solvency/liabilities (X-Internal-Token)
func (s *Server) hSolvency(w http.ResponseWriter, r *http.Request) {
	if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	var wallets, iso, mm decimal.Decimal
	mmUser := common.Env("MM_USER_ID", "mm:main")
	err := s.db.QueryRowContext(r.Context(), `SELECT
		COALESCE((SELECT SUM(balance) FROM fut_accounts), 0),
		COALESCE((SELECT SUM(iso_margin) FROM fut_positions), 0),
		COALESCE((SELECT SUM(balance) FROM fut_accounts WHERE user_id = $1), 0)
		  + COALESCE((SELECT SUM(iso_margin) FROM fut_positions WHERE user_id = $1), 0)`, mmUser).Scan(&wallets, &iso, &mm)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": []map[string]string{{
		"asset": "USDT", "total": wallets.Add(iso).String(), "marketMaker": mm.String(),
	}}})
}
