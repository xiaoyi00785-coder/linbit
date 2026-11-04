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

// usercredit.go: admin_testnet_credit — an admin credits VIRTUAL testnet funds
// to a normal user's FUNDING account (e.g. for acceptance testing).
//
// HARD testnet-only (common.UserCreditAllowed): WALLET_CHAIN=testnet and
// ADMIN_USER_CREDIT_ENABLED=true, refused on any mainnet signal even when the
// flag is set. Double entry: debit user:<uid>:funding_free, credit the
// dedicated system:testnet_treasury account (never the market maker). Each
// credit is at most ADMIN_USER_CREDIT_MAX and the UTC-day total at most
// ADMIN_USER_CREDIT_DAILY_MAX (USDT-equivalent; only stablecoins accepted so
// the cap is exact). Idempotent on ref: the same ref with the same params
// returns the original result; with different params -> ErrRefConflict.

// TestnetTreasuryAccount is the ledger counterparty for admin testnet credits.
const TestnetTreasuryAccount = "system:testnet_treasury"

const userCreditOp = "admin_testnet_credit"

// userCreditAssets are accepted 1:1 as USDT-equivalent for the caps.
var userCreditAssets = map[string]bool{"USDT": true, "USDC": true}

var (
	ErrCreditDisabled  = errors.New("admin_testnet_credit disabled")
	ErrRefConflict     = errors.New("ref already used with different parameters")
	ErrCreditCap       = errors.New("credit exceeds per-credit cap")
	ErrCreditDailyCap  = errors.New("credit exceeds daily cap")
	ErrCreditAsset     = errors.New("asset not allowed for user credit")
	ErrCreditMMAccount = errors.New("cannot credit system or market-maker account")
)

// UserCredit is the stored result of one credit.
type UserCredit struct {
	Ref       string `json:"ref"`
	UserID    string `json:"userId"`
	Asset     string `json:"asset"`
	Amount    string `json:"amount"`
	Account   string `json:"account"`
	TxType    string `json:"txType"`
	CreatedAt string `json:"createdAt"`
	Before    string `json:"fundingFreeBefore"`
	After     string `json:"fundingFreeAfter"`
	Replayed  bool   `json:"replayed"`
}

func creditRef(ref string) string { return "admin_credit:" + ref }

func userCreditLimits() (perCredit, daily decimal.Decimal) {
	perCredit = decimal.RequireFromString("100000")
	daily = decimal.RequireFromString("1000000")
	if d, err := decimal.NewFromString(strings.TrimSpace(common.Env("ADMIN_USER_CREDIT_MAX", ""))); err == nil && d.IsPositive() {
		perCredit = d
	}
	if d, err := decimal.NewFromString(strings.TrimSpace(common.Env("ADMIN_USER_CREDIT_DAILY_MAX", ""))); err == nil && d.IsPositive() {
		daily = d
	}
	return
}

func (s *Store) fundingFree(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, userID, asset string) (decimal.Decimal, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT free::text FROM funding_balances WHERE user_id=$1 AND asset=$2`, userID, asset).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.RequireFromString(v), nil
}

// AdminTestnetCredit credits amount of asset to userID's funding free balance.
func (s *Store) AdminTestnetCredit(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) (UserCredit, error) {
	if ok, why := common.UserCreditAllowed(); !ok {
		log.Printf("!!! REFUSED %s user=%s asset=%s amount=%s: %s", userCreditOp, userID, asset, amount, why)
		return UserCredit{}, ErrCreditDisabled
	}
	asset = strings.ToUpper(strings.TrimSpace(asset))
	ref = strings.TrimSpace(ref)
	if amount.Sign() <= 0 || amount.Exponent() < -8 || ref == "" || len(ref) > 128 || userID == "" {
		return UserCredit{}, ErrInvalidAmount
	}
	if strings.HasPrefix(userID, "system:") || strings.HasPrefix(userID, "platform:") || userID == mmUserID() || strings.HasPrefix(userID, "mm:") {
		return UserCredit{}, ErrCreditMMAccount
	}
	if !userCreditAssets[asset] {
		return UserCredit{}, ErrCreditAsset
	}
	maxOne, maxDay := userCreditLimits()
	key := creditRef(ref)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserCredit{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	// Serialise credits so the ref check and the daily cap are race-free.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('admin_testnet_credit'))`); err != nil {
		return UserCredit{}, err
	}
	var pu, pa, pamt string
	var pat time.Time
	err = tx.QueryRowContext(ctx, `SELECT user_id, asset, amount::text, created_at FROM wallet_ops WHERE ref=$1 AND op=$2`, key, userCreditOp).Scan(&pu, &pa, &pamt, &pat)
	switch {
	case err == nil:
		if pu != userID || pa != asset || !decimal.RequireFromString(pamt).Equal(amount) {
			return UserCredit{}, ErrRefConflict
		}
		return UserCredit{Ref: ref, UserID: pu, Asset: pa, Amount: decimal.RequireFromString(pamt).String(), Account: AcctFunding,
			TxType: userCreditOp, CreatedAt: pat.UTC().Format(time.RFC3339), Replayed: true}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return UserCredit{}, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM wallet_ops WHERE ref=$1)`, key).Scan(&exists); err != nil {
		return UserCredit{}, err
	}
	if exists {
		return UserCredit{}, ErrRefConflict
	}
	if amount.GreaterThan(maxOne) {
		return UserCredit{}, ErrCreditCap
	}
	var today string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0)::text FROM wallet_ops
		WHERE op=$1 AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`, userCreditOp).Scan(&today); err != nil {
		return UserCredit{}, err
	}
	if decimal.RequireFromString(today).Add(amount).GreaterThan(maxDay) {
		return UserCredit{}, ErrCreditDailyCap
	}
	before, err := s.fundingFree(ctx, tx, userID, asset)
	if err != nil {
		return UserCredit{}, err
	}
	if err := claimRef(ctx, tx, key, userCreditOp, userID, asset, amount); err != nil {
		return UserCredit{}, err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: amount, acct: AcctFunding}}); err != nil {
		return UserCredit{}, err
	}
	lines := []ledgerLine{
		{Account: acctBucket(userID, AcctFunding, "free"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: TestnetTreasuryAccount, Asset: asset, Direction: "credit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, userID, userCreditOp, key, lines); err != nil {
		return UserCredit{}, err
	}
	after, err := s.fundingFree(ctx, tx, userID, asset)
	if err != nil {
		return UserCredit{}, err
	}
	if err := tx.Commit(); err != nil {
		return UserCredit{}, err
	}
	log.Printf("!!! VIRTUAL (UNBACKED) %s user=%s asset=%s amount=%s ref=%s", userCreditOp, userID, asset, amount, ref)
	return UserCredit{Ref: ref, UserID: userID, Asset: asset, Amount: amount.String(), Account: AcctFunding, TxType: userCreditOp,
		CreatedAt: time.Now().UTC().Format(time.RFC3339), Before: before.String(), After: after.String()}, nil
}

// POST /internal/admin/user-credit {userId, asset, amount, ref}
func (s *Server) handleUserCredit(w http.ResponseWriter, r *http.Request) {
	if ok, why := common.UserCreditAllowed(); !ok {
		common.WriteError(w, http.StatusForbidden, "USER_CREDIT_DISABLED", "测试网充值已禁用（"+why+"）")
		return
	}
	var req struct {
		UserID string `json:"userId"`
		Asset  string `json:"asset"`
		Amount string `json:"amount"`
		Ref    string `json:"ref"`
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
	res, err := s.store.AdminTestnetCredit(r.Context(), strings.TrimSpace(req.UserID), req.Asset, amount, req.Ref)
	switch {
	case err == nil:
		common.WriteJSON(w, http.StatusOK, res)
	case errors.Is(err, ErrCreditDisabled):
		common.WriteError(w, http.StatusForbidden, "USER_CREDIT_DISABLED", "测试网充值已禁用")
	case errors.Is(err, ErrRefConflict):
		common.WriteError(w, http.StatusConflict, "REF_CONFLICT", "ref 已被使用且参数不同")
	case errors.Is(err, ErrCreditCap):
		common.WriteError(w, http.StatusBadRequest, "CREDIT_CAP_EXCEEDED", "超过单笔上限")
	case errors.Is(err, ErrCreditDailyCap):
		common.WriteError(w, http.StatusBadRequest, "CREDIT_DAILY_CAP_EXCEEDED", "超过当日总额上限")
	case errors.Is(err, ErrCreditAsset):
		common.WriteError(w, http.StatusBadRequest, "ASSET_NOT_ALLOWED", "仅支持 USDT/USDC")
	case errors.Is(err, ErrCreditMMAccount):
		common.WriteError(w, http.StatusForbidden, "ACCOUNT_NOT_ALLOWED", "不能为系统或做市账户充值")
	case errors.Is(err, ErrInvalidAmount):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
	default:
		log.Printf("user credit: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
}
