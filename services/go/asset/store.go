package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

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

// ErrAlreadySettled indicates the trade was already settled (idempotent no-op).
var ErrAlreadySettled = errors.New("trade already settled")

// SettleRequest describes a single matched trade to settle atomically.
//
// Buyer pays QuoteAmount from locked quote and receives BaseQty of base (minus
// BuyerFeeBase). Seller delivers BaseQty from locked base and receives
// QuoteAmount of quote (minus SellerFeeQuote). Both fees go to FeeAccount.
type SettleRequest struct {
	TradeID        string
	Base           string
	Quote          string
	BuyerUserID    string
	SellerUserID   string
	FeeAccount     string
	QuoteAmount    decimal.Decimal // buyer pays / seller receives (gross)
	BaseQty        decimal.Decimal // seller delivers / buyer receives (gross)
	BuyerFeeBase   decimal.Decimal // taken from buyer's received base
	SellerFeeQuote decimal.Decimal // taken from seller's received quote
}

// balanceDelta accumulates per (user, asset) changes to free/locked.
type balanceDelta struct {
	userID string
	asset  string
	free   decimal.Decimal
	locked decimal.Decimal
	acct   string // "" / "spot" = balances, "funding" = funding_balances
}

// Settle applies one matched trade in a single transaction with double-entry
// bookkeeping, idempotent on TradeID. It returns ErrAlreadySettled (which
// callers treat as success) if the trade was settled before.
func (s *Store) Settle(ctx context.Context, req SettleRequest) error {
	if req.QuoteAmount.Sign() <= 0 || req.BaseQty.Sign() <= 0 {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Idempotency gate: if the trade id is already recorded, do nothing.
	res, err := tx.ExecContext(ctx,
		`INSERT INTO settlements (trade_id) VALUES ($1) ON CONFLICT DO NOTHING`, req.TradeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlreadySettled
	}

	buyerRecvBase := req.BaseQty.Sub(req.BuyerFeeBase)
	sellerRecvQuote := req.QuoteAmount.Sub(req.SellerFeeQuote)

	// Net balance deltas. Quote: buyer locked down, seller free up, platform up.
	// Base: seller locked down, buyer free up, platform up.
	deltas := []balanceDelta{
		{userID: req.BuyerUserID, asset: req.Quote, locked: req.QuoteAmount.Neg()},
		{userID: req.SellerUserID, asset: req.Quote, free: sellerRecvQuote},
		{userID: req.FeeAccount, asset: req.Quote, free: req.SellerFeeQuote},
		{userID: req.SellerUserID, asset: req.Base, locked: req.BaseQty.Neg()},
		{userID: req.BuyerUserID, asset: req.Base, free: buyerRecvBase},
		{userID: req.FeeAccount, asset: req.Base, free: req.BuyerFeeBase},
	}
	if err := s.applyDeltas(ctx, tx, deltas); err != nil {
		return err
	}

	// Double-entry journal. Debit = bucket increases, credit = bucket decreases;
	// each asset must net to zero. Zero-amount legs (e.g. a zero fee) are skipped
	// to satisfy the amount > 0 ledger constraint.
	lines := []ledgerLine{}
	add := func(acct, asset, dir string, amt decimal.Decimal) {
		if amt.Sign() > 0 {
			lines = append(lines, ledgerLine{Account: acct, Asset: asset, Direction: dir, Amount: amt})
		}
	}
	add(userAcct(req.BuyerUserID, "locked"), req.Quote, "credit", req.QuoteAmount)
	add(userAcct(req.SellerUserID, "free"), req.Quote, "debit", sellerRecvQuote)
	add(userAcct(req.FeeAccount, "free"), req.Quote, "debit", req.SellerFeeQuote)
	add(userAcct(req.SellerUserID, "locked"), req.Base, "credit", req.BaseQty)
	add(userAcct(req.BuyerUserID, "free"), req.Base, "debit", buyerRecvBase)
	add(userAcct(req.FeeAccount, "free"), req.Base, "debit", req.BuyerFeeBase)

	if err := validateBalanced(lines); err != nil {
		return err
	}
	var txID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO ledger_transactions (user_id, tx_type, ref) VALUES ($1, $2, $3) RETURNING id`,
		req.BuyerUserID, "settle", nullIfEmpty(req.TradeID)).Scan(&txID); err != nil {
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

// applyDeltas locks and updates each (user, asset) balance row inside tx. Rows
// are touched in a deterministic order (user, then asset) to avoid deadlocks,
// and the non-negative invariant is enforced per bucket.
func (s *Store) applyDeltas(ctx context.Context, tx *sql.Tx, deltas []balanceDelta) error {
	// Merge duplicates and sort for a stable lock order.
	merged := map[string]*balanceDelta{}
	for _, d := range deltas {
		if d.acct == AcctSpot {
			d.acct = ""
		}
		key := d.acct + "\x00" + d.userID + "\x00" + d.asset
		if m, ok := merged[key]; ok {
			m.free = m.free.Add(d.free)
			m.locked = m.locked.Add(d.locked)
		} else {
			cp := d
			merged[key] = &cp
		}
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		d := merged[k]
		table := "balances"
		if d.acct == AcctFunding {
			table = "funding_balances"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO asset_accounts (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, d.userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO `+table+` (user_id, asset) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			d.userID, d.asset); err != nil {
			return err
		}
		var freeStr, lockedStr string
		if err := tx.QueryRowContext(ctx,
			`SELECT free::text, locked::text FROM `+table+`
			 WHERE user_id = $1 AND asset = $2 FOR UPDATE`,
			d.userID, d.asset).Scan(&freeStr, &lockedStr); err != nil {
			return err
		}
		newFree := decimal.RequireFromString(freeStr).Add(d.free)
		newLocked := decimal.RequireFromString(lockedStr).Add(d.locked)
		if newFree.IsNegative() || newLocked.IsNegative() {
			return ErrInsufficient
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+table+` SET free = $3, locked = $4 WHERE user_id = $1 AND asset = $2`,
			d.userID, d.asset, newFree.String(), newLocked.String()); err != nil {
			return err
		}
	}
	return nil
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
