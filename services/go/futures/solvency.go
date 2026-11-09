package main

// 给每日偿付检查的合约侧负债。划入合约的资金已离开现货 balances，所以 fut_accounts.balance
// （含系统账户）加上逐仓保证金都要算上。未实现盈亏是零和，不计入。

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// GET /internal/solvency/liabilities，校验 X-Internal-Token。做市账户 MM_USER_ID（默认 mm:main）单独列出。
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
