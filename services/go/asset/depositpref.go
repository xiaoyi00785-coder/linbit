package main

// depositpref.go：用户自己的默认充值账户。没有记录就用平台设置。
// 改这个偏好不搬余额，也不给任何一笔充值入账。合约保证金不在选项里。

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

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
		// 链上入账只记资金账户或现货。合约保证金在 fut_accounts，今天要先从资金/现货扣再转入。
		// 没有一笔幂等交易能只加保证金、不扣这边的余额，所以不提供合约。静默落到资金账户会记错钱包。
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
