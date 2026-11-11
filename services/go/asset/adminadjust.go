package main

// adminadjust.go：人工调账 admin_adjust，改用户现货或资金账户的可用余额，主网可用。
// 双人复核、2FA、阈值和审计在 admin；这里只做一笔原子复式记账。
// 对手账户 system:admin_adjust。加钱：借用户 free、贷 system:admin_adjust；减钱相反。
// 减钱不能把 free 打成负数（applyDeltas → ErrInsufficient）。
// 幂等键 wallet_ops 主键 "admin_adjust:<ref>"：同 ref 同参数回放原结果，参数不同 → ErrRefConflict。
// op 为 admin_adjust_credit / admin_adjust_debit，供 Liabilities.AdminAdjustNet 统计无链上充值支撑的净加款。

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

// AdminAdjustAccount 是人工调账的对手账户。净加款没有链上充值支撑，对账要单独看。
const AdminAdjustAccount = "system:admin_adjust"

const (
	adminAdjustTx     = "admin_adjust"
	adminAdjustCredit = "admin_adjust_credit"
	adminAdjustDebit  = "admin_adjust_debit"
)

var ErrAdjustAccount = errors.New("adjust not allowed for this account")

// AdjustResult 是一笔调账的结果。Amount 为带符号的十进制字符串。
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

// AdminAdjust 按带符号金额调账：>0 加可用，<0 扣可用。只动 free，不动 locked。
func (s *Store) AdminAdjust(ctx context.Context, userID, asset, acct string, amount decimal.Decimal, ref string) (AdjustResult, error) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	ref = strings.TrimSpace(ref)
	if acct == "" {
		acct = AcctSpot
	}
	if !validAcct(acct) {
		return AdjustResult{}, ErrInvalidAccount
	}
	// 金额用 decimal，不用 float。最多 8 位小数（Exponent < -8 拒绝）；ref 最长 128，作幂等键。
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
		// 账户不在 wallet_ops 里，而在分录账户名上，回放时到分录里对。
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

// AdminAdjustNet 按币种返回人工调账净额（加款减扣款）。正数表示没有链上充值支撑的余额。
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

// POST /internal/admin/adjust {userId, asset, account, amount（带符号十进制字符串）, ref}
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
