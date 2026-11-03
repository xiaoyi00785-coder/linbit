package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// funding.go: the funding wallet. Every user has two asset-service wallets:
// spot (table balances, ledger user:<id>:free|locked) and funding (table
// funding_balances, ledger user:<id>:funding_free|funding_locked). On-chain
// deposits credit the deposit_account setting (default funding) and
// withdrawals freeze from withdraw_account (default funding). Spot <-> funding
// moves are a single balanced journal between the user's own buckets.

const (
	AcctSpot    = "spot"
	AcctFunding = "funding"
)

func validAcct(a string) bool { return a == AcctSpot || a == AcctFunding }

// acctBucket is the ledger account name of a user's bucket in a wallet.
func acctBucket(userID, acct, bucket string) string {
	if acct == AcctFunding {
		return userAcct(userID, "funding_"+bucket)
	}
	return userAcct(userID, bucket)
}

// Setting reads an asset_settings value (def when missing).
func (s *Store) Setting(ctx context.Context, key, def string) string {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM asset_settings WHERE key = $1`, key).Scan(&v); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("asset setting %s: %v", key, err)
		}
		return def
	}
	return v
}

// Settings returns all settings.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM asset_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{"deposit_account": AcctFunding, "withdraw_account": AcctFunding}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(ctx context.Context, key, value, by string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO asset_settings (key, value, updated_by, updated_at) VALUES ($1,$2,$3,now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`, key, value, by)
	return err
}

func (s *Store) depositAccount(ctx context.Context) string {
	if a := s.Setting(ctx, "deposit_account", AcctFunding); validAcct(a) {
		return a
	}
	return AcctFunding
}

func (s *Store) withdrawAccount(ctx context.Context) string {
	if a := s.Setting(ctx, "withdraw_account", AcctFunding); validAcct(a) {
		return a
	}
	return AcctFunding
}

// freezeAccountTx returns the wallet a freeze ref was taken from (spot when unrecorded).
func freezeAccountTx(ctx context.Context, tx *sql.Tx, ref string) (string, error) {
	if ref == "" {
		return AcctSpot, nil
	}
	var a string
	err := tx.QueryRowContext(ctx, `SELECT account FROM freeze_accounts WHERE ref = $1`, ref).Scan(&a)
	if errors.Is(err, sql.ErrNoRows) {
		return AcctSpot, nil
	}
	return a, err
}

func (s *Store) freezeAccount(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return AcctSpot, nil
	}
	var a string
	err := s.db.QueryRowContext(ctx, `SELECT account FROM freeze_accounts WHERE ref = $1`, ref).Scan(&a)
	if errors.Is(err, sql.ErrNoRows) {
		return AcctSpot, nil
	}
	return a, err
}

// FreezeIn moves free -> locked inside the given wallet and records the ref's
// wallet so the matching unfreeze / withdraw-complete hits the same one.
func (s *Store) FreezeIn(ctx context.Context, acct, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.lockMove(ctx, acct, userID, asset, amount, ref, true)
}

// UnfreezeIn moves locked -> free inside the given wallet.
func (s *Store) UnfreezeIn(ctx context.Context, acct, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.lockMove(ctx, acct, userID, asset, amount, ref, false)
}

func (s *Store) lockMove(ctx context.Context, acct, userID, asset string, amount decimal.Decimal, ref string, freeze bool) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	d := balanceDelta{userID: userID, asset: asset, free: amount.Neg(), locked: amount, acct: acct}
	txType := "freeze"
	lines := []ledgerLine{
		{Account: acctBucket(userID, acct, "locked"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: acctBucket(userID, acct, "free"), Asset: asset, Direction: "credit", Amount: amount},
	}
	if !freeze {
		d.free, d.locked, txType = amount, amount.Neg(), "unfreeze"
		lines[0].Direction, lines[1].Direction = "credit", "debit"
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{d}); err != nil {
		return err
	}
	if freeze && ref != "" && acct == AcctFunding {
		if _, err := tx.ExecContext(ctx, `INSERT INTO freeze_accounts (ref, account) VALUES ($1,$2) ON CONFLICT (ref) DO NOTHING`, ref, acct); err != nil {
			return err
		}
	}
	if err := s.writeJournal(ctx, tx, userID, txType, ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// AccountTransfer moves free funds between a user's spot and funding wallets.
// Idempotent on ref (ErrAlreadyApplied on replay).
func (s *Store) AccountTransfer(ctx context.Context, userID, asset, from, to string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if !validAcct(from) || !validAcct(to) || from == to {
		return ErrInvalidAccount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := claimRef(ctx, tx, ref, "account_transfer", userID, asset, amount); err != nil {
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{
		{userID: userID, asset: asset, free: amount.Neg(), acct: from},
		{userID: userID, asset: asset, free: amount, acct: to},
	}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: acctBucket(userID, from, "free"), Asset: asset, Direction: "credit", Amount: amount},
		{Account: acctBucket(userID, to, "free"), Asset: asset, Direction: "debit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, userID, "account_transfer", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// UserTransfer moves free funds from one user's wallet to another user's wallet
// (internal transfer, no fee). Idempotent on ref; the journal balances per asset.
func (s *Store) UserTransfer(ctx context.Context, fromUser, toUser, asset, acct string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if !validAcct(acct) || fromUser == "" || toUser == "" || fromUser == toUser {
		return ErrInvalidAccount
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
		{userID: fromUser, asset: asset, free: amount.Neg(), acct: acct},
		{userID: toUser, asset: asset, free: amount, acct: acct},
	}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: acctBucket(fromUser, acct, "free"), Asset: asset, Direction: "credit", Amount: amount},
		{Account: acctBucket(toUser, acct, "free"), Asset: asset, Direction: "debit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, fromUser, "user_transfer", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// WalletBalances lists every non-empty-or-default balance of one wallet.
func (s *Store) WalletBalances(ctx context.Context, acct, userID string) ([]Balance, error) {
	if acct != AcctFunding {
		b, _, err := s.Balances(ctx, userID, 1, 1000)
		return b, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT asset, free::text, locked::text FROM funding_balances WHERE user_id = $1 ORDER BY asset`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Balance{}
	seen := map[string]bool{}
	for rows.Next() {
		var a, f, l string
		if err := rows.Scan(&a, &f, &l); err != nil {
			return nil, err
		}
		seen[a] = true
		out = append(out, Balance{Asset: a, Free: decimal.RequireFromString(f), Locked: decimal.RequireFromString(l)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, a := range defaultAssets { // same default zero rows as spot
		if !seen[a] {
			out = append(out, Balance{Asset: a})
		}
	}
	sortBalances(out)
	return out, nil
}

// ErrInvalidAccount indicates an unknown or same-wallet transfer.
var ErrInvalidAccount = errors.New("invalid account")

// ---- handlers ----

type acctMoveRequest struct {
	UserID  string `json:"userId"`
	Asset   string `json:"asset"`
	Amount  string `json:"amount"`
	Ref     string `json:"ref"`
	Account string `json:"account"` // spot | funding (optional)
	Purpose string `json:"purpose"` // "withdraw" -> withdraw_account setting
	From    string `json:"from"`
	To      string `json:"to"`
	ToUser  string `json:"toUserId"`
}

func decodeAcctMove(w http.ResponseWriter, r *http.Request) (*acctMoveRequest, decimal.Decimal, bool) {
	var req acctMoveRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return nil, decimal.Zero, false
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return nil, decimal.Zero, false
	}
	req.Asset = strings.ToUpper(req.Asset)
	return &req, amount, true
}

func writeMoveResult(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
	case errors.Is(err, ErrInvalidAccount), errors.Is(err, ErrInvalidAmount):
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "账户或金额不合法")
	case err != nil:
		log.Printf("%s: %v", op, err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// POST /internal/freeze {userId, asset, amount, ref, account?, purpose?}
// Without account/purpose it is the legacy spot freeze (orders, earn, ...).
func (s *Server) handleFreezeAcct(w http.ResponseWriter, r *http.Request) {
	req, amount, ok := decodeAcctMove(w, r)
	if !ok {
		return
	}
	acct := req.Account
	if acct == "" && req.Purpose == "withdraw" {
		acct = s.store.withdrawAccount(r.Context())
	}
	if acct == "" || acct == AcctSpot {
		writeMoveResult(w, "freeze", s.store.Freeze(r.Context(), req.UserID, req.Asset, amount, req.Ref))
		return
	}
	if !validAcct(acct) {
		writeMoveResult(w, "freeze", ErrInvalidAccount)
		return
	}
	writeMoveResult(w, "freeze", s.store.FreezeIn(r.Context(), acct, req.UserID, req.Asset, amount, req.Ref))
}

// POST /internal/unfreeze: releases in the wallet the ref was frozen from
// (explicit account wins; unrecorded refs are spot).
func (s *Server) handleUnfreezeAcct(w http.ResponseWriter, r *http.Request) {
	req, amount, ok := decodeAcctMove(w, r)
	if !ok {
		return
	}
	acct := req.Account
	if acct == "" {
		a, err := s.store.freezeAccount(r.Context(), req.Ref)
		if err != nil {
			writeMoveResult(w, "unfreeze", err)
			return
		}
		acct = a
	}
	if acct == AcctSpot {
		writeMoveResult(w, "unfreeze", s.store.Unfreeze(r.Context(), req.UserID, req.Asset, amount, req.Ref))
		return
	}
	if !validAcct(acct) {
		writeMoveResult(w, "unfreeze", ErrInvalidAccount)
		return
	}
	writeMoveResult(w, "unfreeze", s.store.UnfreezeIn(r.Context(), acct, req.UserID, req.Asset, amount, req.Ref))
}

// POST /internal/account-transfer {userId, asset, amount, from, to, ref}
func (s *Server) handleAccountTransfer(w http.ResponseWriter, r *http.Request) {
	req, amount, ok := decodeAcctMove(w, r)
	if !ok {
		return
	}
	if req.Ref == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 ref")
		return
	}
	writeMoveResult(w, "account-transfer", s.store.AccountTransfer(r.Context(), req.UserID, req.Asset, req.From, req.To, amount, req.Ref))
}

// POST /internal/user-transfer {userId, toUserId, asset, amount, account?, ref}
func (s *Server) handleUserTransfer(w http.ResponseWriter, r *http.Request) {
	req, amount, ok := decodeAcctMove(w, r)
	if !ok {
		return
	}
	if req.Ref == "" || req.ToUser == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 ref / toUserId")
		return
	}
	acct := req.Account
	if acct == "" {
		acct = AcctFunding
	}
	writeMoveResult(w, "user-transfer", s.store.UserTransfer(r.Context(), req.UserID, req.ToUser, req.Asset, acct, amount, req.Ref))
}

// GET/PUT /internal/admin/asset-settings
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.Settings(r.Context())
	if err != nil {
		writeMoveResult(w, "settings", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, settingsDTO(m))
}

func settingsDTO(m map[string]string) map[string]string {
	return map[string]string{"depositAccount": m["deposit_account"], "withdrawAccount": m["withdraw_account"]}
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DepositAccount  *string `json:"depositAccount"`
		WithdrawAccount *string `json:"withdrawAccount"`
		UpdatedBy       string  `json:"updatedBy"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	for _, v := range []*string{req.DepositAccount, req.WithdrawAccount} {
		if v != nil && !validAcct(*v) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "账户必须为 spot 或 funding")
			return
		}
	}
	before, err := s.store.Settings(r.Context())
	if err != nil {
		writeMoveResult(w, "settings", err)
		return
	}
	if req.DepositAccount != nil {
		if err := s.store.SetSetting(r.Context(), "deposit_account", *req.DepositAccount, req.UpdatedBy); err != nil {
			writeMoveResult(w, "settings", err)
			return
		}
	}
	if req.WithdrawAccount != nil {
		if err := s.store.SetSetting(r.Context(), "withdraw_account", *req.WithdrawAccount, req.UpdatedBy); err != nil {
			writeMoveResult(w, "settings", err)
			return
		}
	}
	after, _ := s.store.Settings(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": settingsDTO(before), "after": settingsDTO(after)})
}

func toBalanceDTO(b Balance) balanceDTO {
	return balanceDTO{Asset: b.Asset, Free: b.Free.StringFixed(8), Locked: b.Locked.StringFixed(8), Total: b.Total().StringFixed(8)}
}

func sortBalances(b []Balance) {
	sort.Slice(b, func(i, j int) bool { return b[i].Asset < b[j].Asset })
}
