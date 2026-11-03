package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// ErrInsufficient is returned when an operation would drive a balance negative.
var ErrInsufficient = errors.New("insufficient balance")

// ErrInvalidAmount is returned for non-positive amounts.
var ErrInvalidAmount = errors.New("amount must be positive")

// ErrUnbalanced indicates a programming error: ledger entries do not balance.
var ErrUnbalanced = errors.New("ledger transaction does not balance")

// Balance is a per-asset balance snapshot.
type Balance struct {
	Asset  string
	Free   decimal.Decimal
	Locked decimal.Decimal
}

// Total returns free + locked.
func (b Balance) Total() decimal.Decimal { return b.Free.Add(b.Locked) }

// ledgerLine is one leg of a double-entry transaction.
type ledgerLine struct {
	Account   string // e.g. user:80000001:free, system:external
	Asset     string
	Direction string // debit | credit
	Amount    decimal.Decimal
}

// Store wraps the asset database.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// EnsureAccount creates the asset account and seeds default zero balances.
// Idempotent: safe to call on every register and lazily on first read.
func (s *Store) EnsureAccount(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO asset_accounts (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	for _, asset := range defaultAssets {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO balances (user_id, asset) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			userID, asset); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Balances returns a page of balances plus the total asset count.
func (s *Store) Balances(ctx context.Context, userID string, page, pageSize int) ([]Balance, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM balances WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT asset, free::text, locked::text FROM balances
		 WHERE user_id = $1 ORDER BY asset LIMIT $2 OFFSET $3`,
		userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Balance
	for rows.Next() {
		var asset, free, locked string
		if err := rows.Scan(&asset, &free, &locked); err != nil {
			return nil, 0, err
		}
		out = append(out, Balance{
			Asset:  asset,
			Free:   decimal.RequireFromString(free),
			Locked: decimal.RequireFromString(locked),
		})
	}
	return out, total, rows.Err()
}

// Credit increases a user's free balance (counterparty: system:external).
func (s *Store) Credit(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.post(ctx, userID, asset, "credit", ref, amount, decimal.Zero, []ledgerLine{
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: "system:external", Asset: asset, Direction: "credit", Amount: amount},
	})
}

// Freeze moves funds from free to locked.
func (s *Store) Freeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.post(ctx, userID, asset, "freeze", ref, amount.Neg(), amount, []ledgerLine{
		{Account: userAcct(userID, "locked"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "credit", Amount: amount},
	})
}

// Unfreeze moves funds from locked back to free.
func (s *Store) Unfreeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return s.post(ctx, userID, asset, "unfreeze", ref, amount, amount.Neg(), []ledgerLine{
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: userAcct(userID, "locked"), Asset: asset, Direction: "credit", Amount: amount},
	})
}

// post applies a single balanced transaction: it locks the user's balance row,
// updates free/locked, enforces the non-negative invariant, and writes the
// double-entry journal — all inside one DB transaction.
func (s *Store) post(ctx context.Context, userID, asset, txType, ref string, freeDelta, lockedDelta decimal.Decimal, lines []ledgerLine) error {
	if err := validateBalanced(lines); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Ensure the row exists so it can be locked, then take the row lock.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO balances (user_id, asset) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, asset); err != nil {
		return err
	}
	var freeStr, lockedStr string
	if err := tx.QueryRowContext(ctx,
		`SELECT free::text, locked::text FROM balances
		 WHERE user_id = $1 AND asset = $2 FOR UPDATE`,
		userID, asset).Scan(&freeStr, &lockedStr); err != nil {
		return err
	}

	newFree := decimal.RequireFromString(freeStr).Add(freeDelta)
	newLocked := decimal.RequireFromString(lockedStr).Add(lockedDelta)
	if newFree.IsNegative() || newLocked.IsNegative() {
		return ErrInsufficient
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE balances SET free = $3, locked = $4 WHERE user_id = $1 AND asset = $2`,
		userID, asset, newFree.String(), newLocked.String()); err != nil {
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
	return tx.Commit()
}

// LedgerBalance recomputes a user's net position for an asset directly from the
// journal (debits - credits over the user's buckets). Used to assert that the
// materialised balances table stays consistent with the ledger.
func (s *Store) LedgerBalance(ctx context.Context, userID, asset string) (decimal.Decimal, error) {
	var sum sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN direction = 'debit' THEN amount ELSE -amount END), 0)::text
		 FROM ledger_entries
		 WHERE asset = $1 AND account LIKE $2`,
		asset, "user:"+userID+":%").Scan(&sum)
	if err != nil {
		return decimal.Zero, err
	}
	if !sum.Valid {
		return decimal.Zero, nil
	}
	return decimal.RequireFromString(sum.String), nil
}

// validateBalanced ensures sum(debit) == sum(credit) for each asset.
func validateBalanced(lines []ledgerLine) error {
	perAsset := map[string]decimal.Decimal{}
	for _, l := range lines {
		if l.Amount.Sign() <= 0 {
			return ErrInvalidAmount
		}
		cur := perAsset[l.Asset]
		switch l.Direction {
		case "debit":
			perAsset[l.Asset] = cur.Add(l.Amount)
		case "credit":
			perAsset[l.Asset] = cur.Sub(l.Amount)
		default:
			return fmt.Errorf("%w: bad direction %q", ErrUnbalanced, l.Direction)
		}
	}
	for asset, net := range perAsset {
		if !net.IsZero() {
			return fmt.Errorf("%w: asset %s nets to %s", ErrUnbalanced, asset, net)
		}
	}
	return nil
}

func userAcct(userID, bucket string) string { return "user:" + userID + ":" + bucket }

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
