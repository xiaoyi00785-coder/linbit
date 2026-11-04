package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// adminadjust.go: admin_adjust — a manual balance adjustment (人工调账) on a
// user's spot or funding free balance, mainnet-capable. Maker-checker, 2FA,
// threshold and audit live in the admin service; this is only the atomic
// ledger write.
//
//   - Double entry against system:admin_adjust. Credit: debit the user's free
//     bucket, credit system:admin_adjust. Debit: the reverse.
//   - A debit can never make free negative (applyDeltas -> ErrInsufficient).
//   - Idempotent on ref (wallet_ops PK "admin_adjust:<ref>"): the same ref and
//     params replay the original result; different params -> ErrRefConflict.
//   - wallet_ops op is admin_adjust_credit / admin_adjust_debit so
//     reconciliation can report the net manual credit per asset (unbacked by
//     any on-chain deposit) — see Liabilities.AdminAdjustNet.

const AdminAdjustAccount = "system:admin_adjust"

const (
	adminAdjustTx     = "admin_adjust"
	adminAdjustCredit = "admin_adjust_credit"
	adminAdjustDebit  = "admin_adjust_debit"
)

var ErrAdjustAccount = errors.New("adjust not allowed for this account")

// AdjustResult is the outcome of one adjustment.
type AdjustResult struct {
	Ref      string `json:"ref"`
	UserID   string `json:"userId"`
	Asset    string `json:"asset"`
	Account  string `json:"account"`
	Amount   string `json:"amount"` // signed
	TxType   string `json:"txType"`
	Before   string `json:"freeBefore"`
	After    string `json:"freeAfter"`
	Replayed bool   `json:"replayed"`
	At       string `json:"createdAt"`
}

func adjustRef(ref string) string { return "admin_adjust:" + ref }

func (s *Store) acctFree(ctx context.Context, tx *sql.Tx, acct, userID, asset string) (decimal.Decimal, error) {
	table := "balances"
	if acct == AcctFunding {
		table = "funding_balances"
	}
	var v string
	err := tx.QueryRowContext(ctx, `SELECT free::text FROM `+table+` WHERE user_id=$1 AND asset=$2`, userID, asset).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.RequireFromString(v), nil
}

// AdminAdjust applies a signed adjustment (amount > 0 credit, < 0 debit).
func (s *Store) AdminAdjust(ctx context.Context, userID, asset, acct string, amount decimal.Decimal, ref string) (AdjustResult, error) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	ref = strings.TrimSpace(ref)
	if acct == "" {
		acct = AcctSpot
	}
	if !validAcct(acct) {
		return AdjustResult{}, ErrInvalidAccount
	}
	if amount.Sign() == 0 || amount.Exponent() < -8 || ref == "" || len(ref) > 128 || userID == "" || asset == "" || len(asset) > 16 {
		return AdjustResult{}, ErrInvalidAmount
	}
	if strings.HasPrefix(userID, "system:") || strings.HasPrefix(userID, "platform:") || strings.HasPrefix(userID, "mm:") || userID == mmUserID() {
		return AdjustResult{}, ErrAdjustAccount
	}
	abs, op := amount.Abs(), adminAdjustCredit
	if amount.Sign() < 0 {
		op = adminAdjustDebit
	}
	key := adjustRef(ref)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdjustResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	var pop, pu, pa, pamt string
	var pat time.Time
	err = tx.QueryRowContext(ctx, `SELECT op, user_id, asset, amount::text, created_at FROM wallet_ops WHERE ref=$1`, key).Scan(&pop, &pu, &pa, &pamt, &pat)
	switch {
	case err == nil:
		if pop != op || pu != userID || pa != asset || !decimal.RequireFromString(pamt).Equal(abs) {
			return AdjustResult{}, ErrRefConflict
		}
		// Account is part of the journal, not wallet_ops: compare it there.
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ledger_entries e JOIN ledger_transactions t ON t.id=e.tx_id
			WHERE t.ref=$1 AND e.account=$2`, key, acctBucket(userID, acct, "free")).Scan(&n); err != nil {
			return AdjustResult{}, err
		}
		if n == 0 {
			return AdjustResult{}, ErrRefConflict
		}
		return AdjustResult{Ref: ref, UserID: userID, Asset: asset, Account: acct, Amount: amount.String(), TxType: adminAdjustTx,
			Replayed: true, At: pat.UTC().Format(time.RFC3339)}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return AdjustResult{}, err
	}
	if err := claimRef(ctx, tx, key, op, userID, asset, abs); err != nil {
		if errors.Is(err, ErrAlreadyApplied) { // lost a race with the same ref
			return AdjustResult{}, ErrRefConflict
		}
		return AdjustResult{}, err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: decimal.Zero, acct: acct}}); err != nil {
		return AdjustResult{}, err
	}
	before, err := s.acctFree(ctx, tx, acct, userID, asset)
	if err != nil {
		return AdjustResult{}, err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: amount, acct: acct}}); err != nil {
		return AdjustResult{}, err
	}
	lines := []ledgerLine{
		{Account: acctBucket(userID, acct, "free"), Asset: asset, Direction: "debit", Amount: abs},
		{Account: AdminAdjustAccount, Asset: asset, Direction: "credit", Amount: abs},
	}
	if op == adminAdjustDebit {
		lines[0].Direction, lines[1].Direction = "credit", "debit"
	}
	if err := s.writeJournal(ctx, tx, userID, adminAdjustTx, key, lines); err != nil {
		return AdjustResult{}, err
	}
	after, err := s.acctFree(ctx, tx, acct, userID, asset)
	if err != nil {
		return AdjustResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdjustResult{}, err
	}
	log.Printf("admin_adjust user=%s acct=%s asset=%s amount=%s ref=%s", userID, acct, asset, amount, ref)
	return AdjustResult{Ref: ref, UserID: userID, Asset: asset, Account: acct, Amount: amount.String(), TxType: adminAdjustTx,
		Before: before.String(), After: after.String(), At: time.Now().UTC().Format(time.RFC3339)}, nil
}

// AdminAdjustNet returns, per asset, credits minus debits of every manual
// adjustment (positive = balance created without an on-chain deposit).
func (s *Store) AdminAdjustNet(ctx context.Context) (map[string]decimal.Decimal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT asset, COALESCE(SUM(CASE WHEN op=$1 THEN amount ELSE -amount END),0)::text
		FROM wallet_ops WHERE op IN ($1,$2) GROUP BY asset`, adminAdjustCredit, adminAdjustDebit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]decimal.Decimal{}
	for rows.Next() {
		var a, v string
		if err := rows.Scan(&a, &v); err != nil {
			return nil, err
		}
		out[a] = decimal.RequireFromString(v)
	}
	return out, rows.Err()
}

// POST /internal/admin/adjust {userId, asset, account, amount (signed), ref}
func (s *Server) handleAdminAdjust(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string `json:"userId"`
		Asset   string `json:"asset"`
		Account string `json:"account"`
		Amount  string `json:"amount"`
		Ref     string `json:"ref"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	res, err := s.store.AdminAdjust(r.Context(), strings.TrimSpace(req.UserID), req.Asset, strings.TrimSpace(req.Account), amount, req.Ref)
	switch {
	case err == nil:
		common.WriteJSON(w, http.StatusOK, res)
	case errors.Is(err, ErrRefConflict):
		common.WriteError(w, http.StatusConflict, "REF_CONFLICT", "ref 已被使用且参数不同")
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "扣减后余额不能为负")
	case errors.Is(err, ErrAdjustAccount):
		common.WriteError(w, http.StatusForbidden, "ACCOUNT_NOT_ALLOWED", "不能调整系统或做市账户")
	case errors.Is(err, ErrInvalidAccount), errors.Is(err, ErrInvalidAmount):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "账户或金额不合法")
	default:
		log.Printf("admin adjust: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
}
