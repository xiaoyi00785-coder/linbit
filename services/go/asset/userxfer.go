package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// userxfer.go: atomic, idempotent free-balance transfer between two users
// (used by account for parent <-> sub-account spot transfers). Both legs are
// journaled in one balanced ledger transaction (tx_type "user_transfer").

func (s *Store) UserTransfer(ctx context.Context, fromUser, toUser, asset string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if fromUser == toUser || ref == "" {
		return errors.New("invalid transfer")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := claimRef(ctx, tx, ref, "user_transfer", fromUser, asset, amount); err != nil {
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{
		{userID: fromUser, asset: asset, free: amount.Neg()},
		{userID: toUser, asset: asset, free: amount},
	}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: userAcct(fromUser, "free"), Asset: asset, Direction: "credit", Amount: amount},
		{Account: userAcct(toUser, "free"), Asset: asset, Direction: "debit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, fromUser, "user_transfer", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// POST /internal/user-transfer {fromUserId, toUserId, asset, amount, ref}
func (s *Server) handleUserTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From   string `json:"fromUserId"`
		To     string `json:"toUserId"`
		Asset  string `json:"asset"`
		Amount string `json:"amount"`
		Ref    string `json:"ref"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.From == "" || req.To == "" || req.From == req.To || req.Asset == "" || req.Ref == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	err = s.store.UserTransfer(r.Context(), req.From, req.To, strings.ToUpper(req.Asset), amount, req.Ref)
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
	case err != nil:
		log.Printf("user transfer: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
