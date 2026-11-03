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

// futuresops.go moves funds between a user's spot (funding) balance and the
// futures service, which keeps its own margin ledger. The counterparty on this
// side is system:futures, so the sum of all futures wallets always equals the
// system:futures liability here. Idempotent on ref.

// FuturesAccount is the platform account mirroring funds held by the futures service.
const FuturesAccount = "system:futures"

// FuturesTransfer moves amount out of (toFutures=true) or back into the user's free balance.
func (s *Store) FuturesTransfer(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string, toFutures bool) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	op, delta := "futures_in", amount
	if toFutures {
		op, delta = "futures_out", amount.Neg()
	}
	if err := claimRef(ctx, tx, ref, op, userID, asset, amount); err != nil {
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: delta}}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "credit", Amount: amount},
		{Account: FuturesAccount, Asset: asset, Direction: "debit", Amount: amount},
	}
	if !toFutures {
		lines[0].Direction, lines[1].Direction = "debit", "credit"
	}
	if err := s.writeJournal(ctx, tx, userID, op, ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) handleFuturesTransfer(toFutures bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req walletMovementRequest
		if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" || req.Ref == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
			return
		}
		amount, err := decimal.NewFromString(req.Amount)
		if err != nil || amount.Sign() <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
			return
		}
		err = s.store.FuturesTransfer(r.Context(), req.UserID, strings.ToUpper(req.Asset), amount, req.Ref, toFutures)
		switch {
		case errors.Is(err, ErrAlreadyApplied):
			common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
		case errors.Is(err, ErrInsufficient):
			common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
		case err != nil:
			log.Printf("futures transfer: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		default:
			common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	}
}
