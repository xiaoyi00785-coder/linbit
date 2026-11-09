package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// Per-user default deposit account. Missing row means the platform setting.
// Changing it does not move balances and does not credit a deposit.

func resolveDepositAccount(pref, platform string) (account, source string) {
	if validAcct(pref) {
		return pref, "user"
	}
	if validAcct(platform) {
		return platform, "platform"
	}
	return AcctFunding, "platform"
}

func (s *Store) userDepositPref(ctx context.Context, userID string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT account FROM user_deposit_accounts WHERE user_id=$1`, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) effectiveDepositAccount(ctx context.Context, userID string) (string, string) {
	pref, err := s.userDepositPref(ctx, userID)
	if err != nil {
		log.Printf("deposit account pref %s: %v", userID, err)
		pref = ""
	}
	return resolveDepositAccount(pref, s.depositAccount(ctx))
}

func (s *Server) handleGetDepositAccount(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	acct, source := s.store.effectiveDepositAccount(r.Context(), uid)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"account": acct, "source": source, "platformDefault": s.store.depositAccount(r.Context()),
		"supported": []string{AcctFunding, AcctSpot},
	})
}

func (s *Server) handlePutDepositAccount(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req struct {
		Account string `json:"account"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "account 仅支持 funding 或 spot")
		return
	}
	if req.Account == "futures" {
		// On-chain credit posts the asset ledger (funding/spot) only. Futures
		// margin is a separate fut_accounts balance, reached today by an explicit
		// transfer that first debits funding/spot. There is no single idempotent
		// transaction that credits available margin without that debit, so futures
		// is not offered. A silent fallback into funding would book the wrong wallet.
		common.WriteJSON(w, http.StatusConflict, map[string]any{
			"code": "DEPOSIT_ACCOUNT_UNSUPPORTED", "message": "链上充值还不能直接进入合约账户",
			"supported": []string{AcctFunding, AcctSpot}, "account": req.Account, "requestId": common.RequestID(),
		})
		return
	}
	if !validAcct(req.Account) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "account 仅支持 funding 或 spot")
		return
	}
	if _, err := s.store.db.ExecContext(r.Context(), `INSERT INTO user_deposit_accounts (user_id, account, updated_at) VALUES ($1,$2,now())
		ON CONFLICT (user_id) DO UPDATE SET account=EXCLUDED.account, updated_at=now()`, uid, req.Account); err != nil {
		log.Printf("put deposit account: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.handleGetDepositAccount(w, r)
}

func (s *Server) handleDeleteDepositAccount(w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	if _, err := s.store.db.ExecContext(r.Context(), `DELETE FROM user_deposit_accounts WHERE user_id=$1`, uid); err != nil {
		log.Printf("delete deposit account: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.handleGetDepositAccount(w, r)
}
