package main

import (
	"context"
	"database/sql"
	"errors"

	"github.com/shopspring/decimal"
)

// walletops.go implements the internal, idempotent ledger operations the wallet
// service drives: crediting a confirmed on-chain deposit and finalising a
// confirmed withdrawal. Both are keyed by a caller-supplied ref and applied at
// most once (see the wallet_ops table).

// ErrAlreadyApplied indicates the ref was already posted (idempotent no-op).
var ErrAlreadyApplied = errors.New("operation already applied")

// claimRef inserts ref into wallet_ops inside tx. It returns ErrAlreadyApplied
// when the ref already exists.
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

// Deposit credits a confirmed on-chain deposit to the user's free balance
// (counterparty: system:external). Idempotent on ref.
func (s *Store) Deposit(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.DepositTo(ctx, AcctSpot, userID, asset, amount, ref)
}

// DepositTo credits a confirmed on-chain deposit to the given wallet (spot or
// funding). Idempotent on ref.
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

// WithdrawComplete finalises a confirmed withdrawal. The user's locked balance is
// reduced by amount (the full frozen withdrawal amount minus gas) plus gas — i.e.
// the whole frozen balance is consumed: `amount` leaves as the on-chain transfer
// and `gas` is booked to feeAccount (chain cost). Idempotent on ref.
//
// Ledger (per asset): credit user locked by (amount+gas); debit
// system:external by amount (funds leave the platform) and feeAccount by gas.
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

// writeJournal validates and writes a balanced double-entry journal inside tx.
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
