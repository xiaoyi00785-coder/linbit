package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ErrNotFound is returned when a row lookup misses.
var ErrNotFound = errors.New("not found")

// Internal withdrawal statuses (the state machine). The API maps these onto the
// openapi enum via apiStatus().
const (
	wStatusReview     = "pending_review" // awaiting manual review
	wStatusApproved   = "approved"       // released (auto or admin); ready to sign+broadcast
	wStatusProcessing = "processing"     // signed & broadcast; awaiting confirmations
	wStatusCompleted  = "completed"      // confirmed; funds debited from locked
	wStatusRejected   = "rejected"       // admin rejected; funds unfrozen
	wStatusCanceled   = "canceled"       // user canceled; funds unfrozen
	wStatusFailed     = "failed"         // broadcast failed; funds unfrozen
)

// Deposit statuses.
const (
	depConfirming = "confirming"
	depCompleted  = "completed"
)

// Store wraps the wallet database.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ---- types ----

// DepositAddress is an allocated, reusable deposit address.
type DepositAddress struct {
	UserID  string
	Asset   string
	Network string
	Address string
	Memo    string
}

// Deposit is a credited (or confirming) deposit record.
type Deposit struct {
	ID            int64
	UserID        string
	Asset         string
	Network       string
	Address       string
	Amount        decimal.Decimal
	TxHash        string
	LogIndex      int64
	Confirmations int64
	Status        string
	CreatedAt     time.Time
}

// Withdrawal is a withdrawal request and its state-machine row.
type Withdrawal struct {
	ID            int64
	UserID        string
	Asset         string
	Network       string
	Address       string
	Memo          string
	Amount        decimal.Decimal
	GasFee        decimal.Decimal
	GasFeeAsset   string
	GasDeductMode string
	FrozenAmount  decimal.Decimal
	RiskScore     int
	Status        string
	TxHash        sql.NullString
	RejectReason  sql.NullString
	CreatedAt     time.Time
}

// scanTx is a simulated chain deposit matched to an allocated address.
type scanTx struct {
	UserID        string
	Asset         string
	Address       string
	Amount        decimal.Decimal
	TxHash        string
	LogIndex      int64
	Confirmations int64
}

// ---- deposit addresses ----

// UpsertDepositAddress allocates (or returns the existing) reusable deposit
// address for a (user, asset, network). generate is only consulted on first use.
func (s *Store) UpsertDepositAddress(ctx context.Context, userID, asset, network string, generate func() (string, string)) (DepositAddress, error) {
	addr, memo := generate()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deposit_addresses (user_id, asset, network, address, memo)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, asset, network) DO NOTHING`,
		userID, asset, network, addr, memo)
	if err != nil {
		return DepositAddress{}, err
	}
	var out DepositAddress
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id, asset, network, address, memo FROM deposit_addresses
		 WHERE user_id = $1 AND asset = $2 AND network = $3`,
		userID, asset, network).Scan(&out.UserID, &out.Asset, &out.Network, &out.Address, &out.Memo)
	return out, err
}

// ---- simulated chain ----

// ChainHeight returns the current block height for a network (0 if unseen).
func (s *Store) ChainHeight(ctx context.Context, network string) (int64, error) {
	var h int64
	err := s.db.QueryRowContext(ctx, `SELECT height FROM chain_state WHERE network = $1`, network).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return h, err
}

// MineBlock advances a network's height by one and returns the new height.
func (s *Store) MineBlock(ctx context.Context, network string) (int64, error) {
	return s.AdvanceChain(ctx, network, 1)
}

// AdvanceChain advances a network's height by blocks and returns the new height.
func (s *Store) AdvanceChain(ctx context.Context, network string, blocks int64) (int64, error) {
	if blocks < 1 {
		blocks = 1
	}
	var h int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO chain_state (network, height) VALUES ($1, $2)
		 ON CONFLICT (network) DO UPDATE SET height = chain_state.height + $2
		 RETURNING height`,
		network, blocks).Scan(&h)
	return h, err
}

// InjectChainTx records a simulated incoming deposit, mining it into a new block.
// Idempotent on (network, tx_hash, log_index): a duplicate injection is a no-op
// and does not mine a block. Returns whether a new tx was inserted.
func (s *Store) InjectChainTx(ctx context.Context, network, address, asset string, amount decimal.Decimal, txHash string, logIndex int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck

	var exists int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM chain_txs WHERE network = $1 AND tx_hash = $2 AND log_index = $3`,
		network, txHash, logIndex).Scan(&exists)
	if err == nil {
		return false, nil // duplicate: no-op
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var height int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO chain_state (network, height) VALUES ($1, 1)
		 ON CONFLICT (network) DO UPDATE SET height = chain_state.height + 1
		 RETURNING height`, network).Scan(&height); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO chain_txs (network, address, asset, amount, tx_hash, log_index, block_height)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		network, address, asset, amount.String(), txHash, logIndex, height); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ConfirmableChainTxs returns simulated deposits on a network that (a) target an
// allocated deposit address and (b) now have at least `confirmations`. Already
// completed deposits are excluded so a tx is never credited twice.
func (s *Store) ConfirmableChainTxs(ctx context.Context, network string, confirmations int) ([]ChainDeposit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ct.address, ct.asset, ct.amount::text, ct.tx_hash, ct.log_index,
		        (cs.height - ct.block_height + 1) AS confs
		 FROM chain_txs ct
		 JOIN chain_state cs ON cs.network = ct.network
		 JOIN deposit_addresses da ON da.network = ct.network AND da.address = ct.address
		 WHERE ct.network = $1
		   AND (cs.height - ct.block_height + 1) >= $2
		   AND NOT EXISTS (
		       SELECT 1 FROM deposits d
		       WHERE d.network = ct.network AND d.tx_hash = ct.tx_hash
		         AND d.log_index = ct.log_index AND d.status = 'completed')`,
		network, confirmations)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChainDeposit
	for rows.Next() {
		var d ChainDeposit
		var amt string
		if err := rows.Scan(&d.Address, &d.Asset, &amt, &d.TxHash, &d.LogIndex, &d.Confirmations); err != nil {
			return nil, err
		}
		d.Network = network
		d.Amount = decimal.RequireFromString(amt)
		out = append(out, d)
	}
	return out, rows.Err()
}

// MatchedChainTxs returns every simulated deposit matched to an allocated address
// (regardless of confirmations) with its owner and current confirmation count.
// Used to surface "confirming" deposits before they are credited.
func (s *Store) MatchedChainTxs(ctx context.Context, network string) ([]scanTx, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT da.user_id, ct.asset, ct.address, ct.amount::text, ct.tx_hash, ct.log_index,
		        GREATEST(cs.height - ct.block_height + 1, 0) AS confs
		 FROM chain_txs ct
		 JOIN chain_state cs ON cs.network = ct.network
		 JOIN deposit_addresses da ON da.network = ct.network AND da.address = ct.address
		 WHERE ct.network = $1`, network)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scanTx
	for rows.Next() {
		var t scanTx
		var amt string
		if err := rows.Scan(&t.UserID, &t.Asset, &t.Address, &amt, &t.TxHash, &t.LogIndex, &t.Confirmations); err != nil {
			return nil, err
		}
		t.Amount = decimal.RequireFromString(amt)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ActiveChainNetworks returns the networks that currently have simulated txs.
func (s *Store) ActiveChainNetworks(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT network FROM chain_txs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---- deposits ----

// UpsertDepositConfirming records (or refreshes) a confirming deposit without
// crediting it. It never downgrades a completed deposit.
func (s *Store) UpsertDepositConfirming(ctx context.Context, d Deposit) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deposits (user_id, asset, network, address, amount, tx_hash, log_index, confirmations, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'confirming')
		 ON CONFLICT (network, tx_hash, log_index) DO UPDATE
		   SET confirmations = EXCLUDED.confirmations
		 WHERE deposits.status <> 'completed'`,
		d.UserID, d.Asset, d.Network, d.Address, d.Amount.String(), d.TxHash, d.LogIndex, d.Confirmations)
	return err
}

// MarkDepositCompleted flips a deposit to completed. Returns true if it applied
// (false if it was already completed) so crediting happens exactly once.
func (s *Store) MarkDepositCompleted(ctx context.Context, network, txHash string, logIndex, confirmations int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE deposits SET status = 'completed', confirmations = $4, credited_at = now()
		 WHERE network = $1 AND tx_hash = $2 AND log_index = $3 AND status <> 'completed'`,
		network, txHash, logIndex, confirmations)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DepositStatus returns the current status of a deposit (exists=false if none).
func (s *Store) DepositStatus(ctx context.Context, network, txHash string, logIndex int64) (string, bool, error) {
	var status string
	err := s.db.QueryRowContext(ctx,
		`SELECT status FROM deposits WHERE network = $1 AND tx_hash = $2 AND log_index = $3`,
		network, txHash, logIndex).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

// ListDeposits returns a page of a user's deposits plus the total count.
func (s *Store) ListDeposits(ctx context.Context, userID, asset string, page, pageSize int) ([]Deposit, int, error) {
	args := []any{userID}
	filter := ""
	if asset != "" {
		filter = " AND asset = $2"
		args = append(args, asset)
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM deposits WHERE user_id = $1`+filter, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, asset, network, address, amount::text, tx_hash, log_index, confirmations, status, created_at
		 FROM deposits WHERE user_id = $1`+filter+
			` ORDER BY created_at DESC, id DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Deposit
	for rows.Next() {
		var d Deposit
		var amt string
		if err := rows.Scan(&d.ID, &d.Asset, &d.Network, &d.Address, &amt, &d.TxHash, &d.LogIndex, &d.Confirmations, &d.Status, &d.CreatedAt); err != nil {
			return nil, 0, err
		}
		d.Amount = decimal.RequireFromString(amt)
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// ---- withdrawals ----

// InsertWithdrawal creates a withdrawal row and returns its id.
func (s *Store) InsertWithdrawal(ctx context.Context, w Withdrawal) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO withdrawals
		   (user_id, asset, network, address, memo, amount, gas_fee, gas_fee_asset,
		    gas_deduct_mode, frozen_amount, risk_score, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		w.UserID, w.Asset, w.Network, w.Address, w.Memo, w.Amount.String(), w.GasFee.String(),
		w.GasFeeAsset, w.GasDeductMode, w.FrozenAmount.String(), w.RiskScore, w.Status).Scan(&id)
	return id, err
}

func (s *Store) scanWithdrawal(row interface{ Scan(...any) error }) (*Withdrawal, error) {
	var w Withdrawal
	var amt, gas, frozen string
	err := row.Scan(&w.ID, &w.UserID, &w.Asset, &w.Network, &w.Address, &w.Memo, &amt, &gas,
		&w.GasFeeAsset, &w.GasDeductMode, &frozen, &w.RiskScore, &w.Status, &w.TxHash, &w.RejectReason, &w.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.Amount = decimal.RequireFromString(amt)
	w.GasFee = decimal.RequireFromString(gas)
	w.FrozenAmount = decimal.RequireFromString(frozen)
	return &w, nil
}

const withdrawalColumns = `id, user_id, asset, network, address, memo, amount::text, gas_fee::text,
	gas_fee_asset, gas_deduct_mode, frozen_amount::text, risk_score, status, tx_hash, reject_reason, created_at`

// GetWithdrawal fetches a withdrawal by id.
func (s *Store) GetWithdrawal(ctx context.Context, id int64) (*Withdrawal, error) {
	return s.scanWithdrawal(s.db.QueryRowContext(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawals WHERE id = $1`, id))
}

// transition atomically moves a withdrawal from one of `from` statuses to `to`,
// optionally setting tx_hash / reject_reason. Returns true if it applied.
func (s *Store) transition(ctx context.Context, id int64, from []string, to string, txHash, rejectReason *string, broadcastHeight *int64) (bool, error) {
	// Build "status = ANY($2)" using a small helper array literal.
	res, err := s.db.ExecContext(ctx,
		`UPDATE withdrawals
		   SET status = $3,
		       tx_hash = COALESCE($4, tx_hash),
		       reject_reason = COALESCE($5, reject_reason),
		       broadcast_height = COALESCE($6, broadcast_height),
		       updated_at = now()
		 WHERE id = $1 AND status = ANY(string_to_array($2, ','))`,
		id, pgTextArray(from), to, nullStr(txHash), nullStr(rejectReason), nullInt(broadcastHeight))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Approve moves pending_review -> approved.
func (s *Store) Approve(ctx context.Context, id int64) (bool, error) {
	return s.transition(ctx, id, []string{wStatusReview}, wStatusApproved, nil, nil, nil)
}

// Reject moves pending_review -> rejected.
func (s *Store) Reject(ctx context.Context, id int64, reason string) (bool, error) {
	return s.transition(ctx, id, []string{wStatusReview}, wStatusRejected, nil, &reason, nil)
}

// Cancel moves pending_review -> canceled (user-initiated; owner checked by caller).
func (s *Store) Cancel(ctx context.Context, id int64) (bool, error) {
	return s.transition(ctx, id, []string{wStatusReview}, wStatusCanceled, nil, nil, nil)
}

// MarkProcessing moves approved -> processing, recording the broadcast tx + height.
func (s *Store) MarkProcessing(ctx context.Context, id int64, txHash string, height int64) (bool, error) {
	return s.transition(ctx, id, []string{wStatusApproved}, wStatusProcessing, &txHash, nil, &height)
}

// MarkCompleted moves processing -> completed.
func (s *Store) MarkCompleted(ctx context.Context, id int64) (bool, error) {
	return s.transition(ctx, id, []string{wStatusProcessing}, wStatusCompleted, nil, nil, nil)
}

// MarkFailed moves approved|processing -> failed.
func (s *Store) MarkFailed(ctx context.Context, id int64, reason string) (bool, error) {
	return s.transition(ctx, id, []string{wStatusApproved, wStatusProcessing}, wStatusFailed, nil, &reason, nil)
}

// FailSubmit moves pending_review -> failed (used when the freeze at submit time
// could not be applied). No funds were frozen so there is nothing to release.
func (s *Store) FailSubmit(ctx context.Context, id int64, reason string) (bool, error) {
	return s.transition(ctx, id, []string{wStatusReview}, wStatusFailed, nil, &reason, nil)
}

// BroadcastHeight returns a processing withdrawal's broadcast height (0 if unset).
func (s *Store) BroadcastHeight(ctx context.Context, id int64) (int64, error) {
	var h sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT broadcast_height FROM withdrawals WHERE id = $1`, id).Scan(&h)
	if err != nil {
		return 0, err
	}
	return h.Int64, nil
}

// ListWithdrawalsByStatus returns up to limit withdrawals in a status (oldest first).
func (s *Store) ListWithdrawalsByStatus(ctx context.Context, status string, limit int) ([]Withdrawal, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawals WHERE status = $1 ORDER BY id ASC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Withdrawal
	for rows.Next() {
		w, err := s.scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// ListWithdrawals returns a page of a user's withdrawals plus the total count.
func (s *Store) ListWithdrawals(ctx context.Context, userID, asset string, page, pageSize int) ([]Withdrawal, int, error) {
	args := []any{userID}
	filter := ""
	if asset != "" {
		filter = " AND asset = $2"
		args = append(args, asset)
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM withdrawals WHERE user_id = $1`+filter, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawals WHERE user_id = $1`+filter+
			` ORDER BY created_at DESC, id DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Withdrawal
	for rows.Next() {
		w, err := s.scanWithdrawal(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *w)
	}
	return out, total, rows.Err()
}

// CountWithdrawals24h counts a user's withdrawals created since `since`.
func (s *Store) CountWithdrawals24h(ctx context.Context, userID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM withdrawals WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&n)
	return n, err
}

// AssetAmount is a (asset, amount) pair used to sum rolling-24h withdrawal usage.
type AssetAmount struct {
	Asset  string
	Amount decimal.Decimal
}

// WithdrawnSince returns the (asset, amount) of the user's withdrawals created at
// or after `since` that count toward the KYC daily limit — i.e. everything except
// those that released their funds (rejected / canceled / failed).
func (s *Store) WithdrawnSince(ctx context.Context, userID string, since time.Time) ([]AssetAmount, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT asset, amount::text FROM withdrawals
		 WHERE user_id = $1 AND created_at >= $2
		   AND status NOT IN ('rejected','canceled','failed')`,
		userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssetAmount
	for rows.Next() {
		var a AssetAmount
		var amt string
		if err := rows.Scan(&a.Asset, &amt); err != nil {
			return nil, err
		}
		a.Amount = decimal.RequireFromString(amt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// HasPriorWithdrawalTo reports whether the user has a prior non-rejected
// withdrawal to the same (asset, network, address) — used as a "new address"
// risk signal.
func (s *Store) HasPriorWithdrawalTo(ctx context.Context, userID, asset, network, address string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM withdrawals
		 WHERE user_id = $1 AND asset = $2 AND network = $3 AND address = $4
		   AND status NOT IN ('rejected','canceled','failed') LIMIT 1`,
		userID, asset, network, address).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
