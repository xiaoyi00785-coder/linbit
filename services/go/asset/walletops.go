package main

// walletops.go：钱包驱动的内部记账，都按调用方给的 ref 最多入账一次（wallet_ops）。
// 充值贷记用户可用，对手 system:external。提现完成把冻结整笔消耗掉：
// amount 离开平台（借 system:external），gas 记到 feeAccount。

import (
	"context"
	"database/sql"
	"errors"

	"github.com/shopspring/decimal"
)

// ErrAlreadyApplied 表示这个 ref 已经入过账，再调是幂等空操作。
var ErrAlreadyApplied = errors.New("operation already applied")

// claimRef 在事务里占住 ref。已存在则 ErrAlreadyApplied。ref 为空表示调用方不要幂等。
func claimRef(ctx context.Context, tx *sql.Tx, ref, op, userID, asset string, amount decimal.Decimal) error {
	if ref == "" {
		return nil // no idempotency requested
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO wallet_ops (ref, op, user_id, asset, amount) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (ref) DO NOTHING`,
		ref, op, userID, asset, amount.String())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlreadyApplied
	}
	return nil
}

// Deposit 把已确认充值记到现货可用。对手账户 system:external。按 ref 幂等。
func (s *Store) Deposit(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.DepositTo(ctx, AcctSpot, userID, asset, amount, ref)
}

// DepositTo 把已确认充值记到指定钱包（spot 或 funding 资金账户）。按 ref 幂等。
func (s *Store) DepositTo(ctx context.Context, acct, userID, asset string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if err := claimRef(ctx, tx, ref, "deposit", userID, asset, amount); err != nil {
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: amount, acct: acct}}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: acctBucket(userID, acct, "free"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: "system:external", Asset: asset, Direction: "credit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, userID, "deposit", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// WithdrawComplete 结算已确认提现。locked 减少 amount+gas，即整笔冻结用完：
// amount 作为链上打出（借 system:external），gas 记到 feeAccount（默认 platform:gas）。按 ref 幂等。
// 分录按币种平衡：贷用户 locked (amount+gas)；借 system:external amount；借 feeAccount gas。
func (s *Store) WithdrawComplete(ctx context.Context, userID, asset string, amount, gas decimal.Decimal, feeAccount, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if gas.Sign() < 0 {
		return ErrInvalidAmount
	}
	if feeAccount == "" {
		feeAccount = "platform:gas"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if err := claimRef(ctx, tx, ref, "withdraw_complete", userID, asset, amount.Add(gas)); err != nil {
		return err
	}
	acct, err := freezeAccountTx(ctx, tx, ref)
	if err != nil {
		return err
	}
	deltas := []balanceDelta{
		{userID: userID, asset: asset, locked: amount.Add(gas).Neg(), acct: acct},
	}
	if gas.Sign() > 0 {
		deltas = append(deltas, balanceDelta{userID: feeAccount, asset: asset, free: gas})
	}
	if err := s.applyDeltas(ctx, tx, deltas); err != nil {
		return err
	}

	lines := []ledgerLine{
		{Account: acctBucket(userID, acct, "locked"), Asset: asset, Direction: "credit", Amount: amount.Add(gas)},
	}
	if amount.Sign() > 0 {
		lines = append(lines, ledgerLine{Account: "system:external", Asset: asset, Direction: "debit", Amount: amount})
	}
	if gas.Sign() > 0 {
		lines = append(lines, ledgerLine{Account: userAcct(feeAccount, "free"), Asset: asset, Direction: "debit", Amount: gas})
	}
	if err := s.writeJournal(ctx, tx, userID, "withdraw", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// writeJournal 在同一事务里校验并写入平衡的复式分录。借贷不平直接报错，不落库。
func (s *Store) writeJournal(ctx context.Context, tx *sql.Tx, userID, txType, ref string, lines []ledgerLine) error {
	if err := validateBalanced(lines); err != nil {
		return err
	}
	var txID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO ledger_transactions (user_id, tx_type, ref) VALUES ($1, $2, $3) RETURNING id`,
		userID, txType, nullIfEmpty(ref)).Scan(&txID); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ledger_entries (tx_id, account, asset, direction, amount)
			 VALUES ($1, $2, $3, $4, $5)`,
			txID, l.Account, l.Asset, l.Direction, l.Amount.String()); err != nil {
			return err
		}
	}
	return nil
}
