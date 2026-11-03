package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// store_chain.go holds the persistence used only by the real-chain adapter:
// HD index allocation, deposit discovery cursors, chain_txs ingestion at real
// heights and the sweep state machine.

// NextHDIndex reserves the next deposit-key index (shared across chains).
func (s *Store) NextHDIndex(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT nextval('deposit_hd_index_seq')`).Scan(&n)
	return n, err
}

// DepositAddressesFor lists allocated deposit addresses on a network. Addresses
// are shared by every asset on the chain, so all of them are watched for asset.
func (s *Store) DepositAddressesFor(ctx context.Context, network, asset string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT address FROM deposit_addresses WHERE network = $1 AND $2 <> ''
		 GROUP BY address ORDER BY min(id)`, network, asset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Cursor returns the stored discovery cursor (ok=false if none yet).
func (s *Store) Cursor(ctx context.Context, network, asset string) (int64, bool, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT next_from FROM chain_cursors WHERE network = $1 AND asset = $2`, network, asset).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return n, err == nil, err
}

// SetCursor stores the discovery cursor.
func (s *Store) SetCursor(ctx context.Context, network, asset string, next int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chain_cursors (network, asset, next_from) VALUES ($1, $2, $3)
		 ON CONFLICT (network, asset) DO UPDATE SET next_from = EXCLUDED.next_from`, network, asset, next)
	return err
}

// RecordChainTx stores an observed on-chain deposit at its real block height.
// Idempotent on (network, tx_hash, log_index).
func (s *Store) RecordChainTx(ctx context.Context, network, address, asset string, amount decimal.Decimal, txHash string, logIndex, height int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO chain_txs (network, address, asset, amount, tx_hash, log_index, block_height)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (network, tx_hash, log_index) DO NOTHING`,
		network, address, asset, amount.String(), txHash, logIndex, height)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetChainHeight records the observed chain tip (never moves backwards).
func (s *Store) SetChainHeight(ctx context.Context, network string, height int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chain_state (network, height) VALUES ($1, $2)
		 ON CONFLICT (network) DO UPDATE SET height = GREATEST(chain_state.height, EXCLUDED.height)`,
		network, height)
	return err
}

// ClaimForBroadcast moves approved -> processing with an empty tx hash BEFORE
// anything is sent, so a crash between send and SetWithdrawalTx can never cause
// a second payout (an empty-hash processing row is left for manual review).
func (s *Store) ClaimForBroadcast(ctx context.Context, id int64) (bool, error) {
	return s.MarkProcessing(ctx, id, "", 0)
}

// SetWithdrawalTx records the broadcast hash of a claimed withdrawal.
func (s *Store) SetWithdrawalTx(ctx context.Context, id int64, txHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE withdrawals SET tx_hash = $2, updated_at = now() WHERE id = $1 AND status = $3`,
		id, txHash, wStatusProcessing)
	return err
}

// ---- sweeps ----

const (
	sweepTopupSent = "topup_sent"
	sweepSent      = "sweep_sent"
	sweepDone      = "done"
	sweepFailed    = "failed"
)

// Sweep is one sweep attempt of a deposit address.
type Sweep struct {
	ID          int64
	Network     string
	Asset       string
	Address     string
	HDIndex     int64
	Stage       string
	Amount      decimal.Decimal
	Destination string
	TopupTx     string
	SweepTx     string
	UpdatedAt   time.Time
}

// SweepCandidate is a deposit address with credited funds not yet swept.
type SweepCandidate struct {
	Network, Asset, Address string
	HDIndex                 int64
}

// SweepCandidates returns deposit addresses with a completed deposit newer than
// their last finished sweep, no active sweep, and no failure in the last hour.
func (s *Store) SweepCandidates(ctx context.Context, limit int) ([]SweepCandidate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.network, d.asset, d.address, min(da.hd_index)
		 FROM (SELECT DISTINCT network, asset, address FROM deposits WHERE status = 'completed') d
		 JOIN deposit_addresses da ON da.network = d.network AND da.address = d.address
		 WHERE da.hd_index IS NOT NULL
		   AND EXISTS (SELECT 1 FROM deposits d2
		               WHERE d2.network = d.network AND d2.address = d.address AND d2.asset = d.asset AND d2.status = 'completed'
		                 AND d2.credited_at > COALESCE((SELECT max(sw.created_at) FROM sweeps sw
		                     WHERE sw.network = d.network AND sw.address = d.address AND sw.asset = d.asset AND sw.stage = 'done'), '-infinity'))
		   AND NOT EXISTS (SELECT 1 FROM sweeps sw WHERE sw.network = d.network AND sw.address = d.address
		                   AND (sw.stage IN ('topup_sent','sweep_sent')
		                        OR (sw.stage = 'failed' AND sw.asset = d.asset AND sw.updated_at > now() - interval '1 hour')))
		 GROUP BY d.network, d.asset, d.address
		 ORDER BY min(da.id) LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SweepCandidate
	for rows.Next() {
		var c SweepCandidate
		if err := rows.Scan(&c.Network, &c.Asset, &c.Address, &c.HDIndex); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertSweep starts a sweep in the given stage.
func (s *Store) InsertSweep(ctx context.Context, sw Sweep) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO sweeps (network, asset, address, hd_index, stage, amount, destination, topup_tx, sweep_tx)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		sw.Network, sw.Asset, sw.Address, sw.HDIndex, sw.Stage, sw.Amount.String(), sw.Destination, sw.TopupTx, sw.SweepTx).Scan(&id)
	return id, err
}

// UpdateSweep moves a sweep to a new stage.
func (s *Store) UpdateSweep(ctx context.Context, id int64, stage, sweepTx, errMsg string, amount decimal.Decimal, dest string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sweeps SET stage = $2, sweep_tx = COALESCE(NULLIF($3, ''), sweep_tx), error = $4,
		        amount = CASE WHEN $5::numeric > 0 THEN $5::numeric ELSE amount END,
		        destination = COALESCE(NULLIF($6, ''), destination), updated_at = now()
		 WHERE id = $1`, id, stage, sweepTx, errMsg, amount.String(), dest)
	return err
}

// ActiveSweeps lists sweeps waiting on a transaction.
func (s *Store) ActiveSweeps(ctx context.Context, limit int) ([]Sweep, error) {
	return s.listSweeps(ctx, `WHERE stage IN ('topup_sent','sweep_sent') ORDER BY id LIMIT $1`, limit)
}

// RecentSweeps lists the latest sweeps (admin view).
func (s *Store) RecentSweeps(ctx context.Context, limit int) ([]Sweep, error) {
	return s.listSweeps(ctx, `ORDER BY id DESC LIMIT $1`, limit)
}

func (s *Store) listSweeps(ctx context.Context, where string, limit int) ([]Sweep, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, network, asset, address, hd_index, stage, amount::text, destination, topup_tx, sweep_tx, updated_at
		 FROM sweeps `+where, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sweep
	for rows.Next() {
		var sw Sweep
		var amt string
		if err := rows.Scan(&sw.ID, &sw.Network, &sw.Asset, &sw.Address, &sw.HDIndex, &sw.Stage, &amt,
			&sw.Destination, &sw.TopupTx, &sw.SweepTx, &sw.UpdatedAt); err != nil {
			return nil, err
		}
		sw.Amount = decimal.RequireFromString(amt)
		out = append(out, sw)
	}
	return out, rows.Err()
}
