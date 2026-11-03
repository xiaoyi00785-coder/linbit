package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// systemops.go: generic, idempotent transfers between a user's free balance
// and a named platform account (system:<account>), used by growth (rebates,
// marketing cashback) and earn (savings principal, loans, collateral).
// Platform accounts are ledger-only liabilities, so the journal still
// balances. Debits fail with INSUFFICIENT_BALANCE when free is too low.

var systemAccounts = map[string]bool{
	"rebate": true, "marketing": true, "earn": true, "earn_interest": true,
	"loan": true, "loan_collateral": true, "loan_interest": true,
	"event": true, // event contracts (futures service): stakes in, payouts/refunds out
}

func (s *Store) SystemTransfer(ctx context.Context, userID, asset, account, direction string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	op := "sys_" + direction + ":" + account
	if err := claimRef(ctx, tx, ref, op, userID, asset, amount); err != nil {
		return err
	}
	delta := amount
	if direction == "debit" {
		delta = amount.Neg()
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: delta}}); err != nil {
		return err
	}
	sys := "system:" + account
	lines := []ledgerLine{ // credit to user: platform debit, user credit
		{Account: sys, Asset: asset, Direction: "debit", Amount: amount},
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "credit", Amount: amount},
	}
	if direction == "debit" {
		lines[0].Direction, lines[1].Direction = "credit", "debit"
	}
	if err := s.writeJournal(ctx, tx, userID, op, ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// POST /internal/system-transfer {userId, asset, amount, account, direction: credit|debit, ref}
func (s *Server) handleSystemTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    string `json:"userId"`
		Asset     string `json:"asset"`
		Amount    string `json:"amount"`
		Account   string `json:"account"`
		Direction string `json:"direction"`
		Ref       string `json:"ref"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" || req.Ref == "" ||
		!systemAccounts[req.Account] || (req.Direction != "credit" && req.Direction != "debit") {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	err = s.store.SystemTransfer(r.Context(), req.UserID, strings.ToUpper(req.Asset), req.Account, req.Direction, amount, req.Ref)
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
	case err != nil:
		log.Printf("system transfer: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// GET /internal/ops/feed?after=<seq>&limit= — wallet_ops in seq order.
func (s *Server) handleOpsFeed(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT seq, ref, op, user_id, asset, amount::text, created_at FROM wallet_ops
		WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	type item struct {
		Seq    int64  `json:"seq"`
		Ref    string `json:"ref"`
		Op     string `json:"op"`
		UserID string `json:"userId"`
		Asset  string `json:"asset"`
		Amount string `json:"amount"`
		Time   int64  `json:"time"`
	}
	items := []item{}
	for rows.Next() {
		var it item
		var at time.Time
		if err := rows.Scan(&it.Seq, &it.Ref, &it.Op, &it.UserID, &it.Asset, &it.Amount, &at); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		it.Time = at.UnixMilli()
		items = append(items, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
