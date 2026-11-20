package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/shopspring/decimal"
)

// settleRewards writes one reward row per position per complete UTC day since
// the position's last_settle_day. The (position_id, epoch) UNIQUE constraint
// makes a retried tick a no-op: the same day cannot be credited twice.
func (s *Server) settleRewards(ctx context.Context) error {
	today := s.dayIndex(s.now())
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM s_positions
		WHERE status IN ('active','unbonding') AND last_settle_day < $1 ORDER BY id LIMIT 1000`, today)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.settleOne(ctx, id); err != nil {
			log.Printf("staking: settle position %d: %v", id, err)
		}
	}
	return nil
}

func (s *Server) settleOne(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	pos, err := s.lockPosition(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := s.settlePosition(ctx, tx, pos); err != nil {
		return err
	}
	return tx.Commit()
}

// settlePosition brings the position's pending_rewards up to the current UTC
// day. The transaction's caller must hold the row lock (loaded with FOR
// UPDATE).
//
// Rewards start the day after started_at (last_settle_day is set to that
// stake-day value by stake()) and stop once the position enters unbonding or
// is released (unbond_at / released_at caps the terminal day). The unique
// (position_id, epoch) index makes this idempotent across retries.
func (s *Server) settlePosition(ctx context.Context, tx *sql.Tx, pos *position) error {
	if pos.Status == "released" || pos.Status == "cancelled" {
		return nil
	}
	today := s.dayIndex(s.now())
	lastDay := today
	// Unbonding stops yielding once unbond_at hits; released is already done.
	if pos.Status == "unbonding" && pos.UnbondAt != nil {
		if d := s.dayIndex(*pos.UnbondAt); d < lastDay {
			lastDay = d
		}
	}
	from := pos.LastSettleDay + 1
	if from > lastDay {
		return nil
	}
	total := decimal.Zero
	now := s.now()
	for k := from; k <= lastDay; k++ {
		r := dailyReward(pos.Amount, pos.APRSnapshot)
		if r.Sign() <= 0 {
			continue
		}
		// Idempotent insert: a prior tick may have written the same (pos, day).
		res, err := tx.ExecContext(ctx, `INSERT INTO s_rewards (position_id, uid, asset, epoch, amount, created_at)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (position_id, epoch) DO NOTHING`,
			pos.ID, pos.UID, pos.Asset, k, r, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			total = total.Add(r)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE s_positions SET last_settle_day=$2, pending_rewards=pending_rewards+$3 WHERE id=$1`,
		pos.ID, lastDay, total); err != nil {
		return err
	}
	pos.LastSettleDay = lastDay
	pos.PendingRewards = pos.PendingRewards.Add(total)
	return nil
}

// releaseUnbonded releases every position whose unbonding window has elapsed.
func (s *Server) releaseUnbonded(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM s_positions
		WHERE status='unbonding' AND unbond_at <= $1 ORDER BY id LIMIT 500`, s.now())
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.releaseOne(ctx, id); err != nil {
			log.Printf("staking: release %d: %v", id, err)
		}
	}
	return nil
}

func (s *Server) releaseOne(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	pos, err := s.lockPosition(ctx, tx, id)
	if err != nil {
		return err
	}
	if pos.Status != "unbonding" {
		return nil
	}
	if err := s.releasePosition(ctx, tx, pos); err != nil {
		return err
	}
	return tx.Commit()
}

// releasePosition pays principal (and any still-pending rewards) back to the
// spot ledger and marks the position released. Idempotent on position id via
// the transfer ref.
func (s *Server) releasePosition(ctx context.Context, tx *sql.Tx, pos *position) error {
	// Seal any remaining whole days (unbonding paths are already capped).
	if err := s.settlePosition(ctx, tx, pos); err != nil {
		return err
	}
	ref := fmt.Sprintf("staking:release:%d", pos.ID)
	if err := s.systemTransfer(ctx, pos.UID, pos.Asset, "staking", "credit", pos.Amount, ref+":principal"); err != nil {
		return err
	}
	rewards := pos.PendingRewards
	if rewards.Sign() > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE s_rewards SET claimed_at=$2 WHERE position_id=$1 AND claimed_at IS NULL`, pos.ID, s.now()); err != nil {
			return err
		}
		if err := s.systemTransfer(ctx, pos.UID, pos.Asset, "staking_rewards", "credit", rewards, ref+":rewards"); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE s_positions SET status='released', released_at=$2,
			claimed_rewards=claimed_rewards+pending_rewards, pending_rewards=0 WHERE id=$1`, pos.ID, s.now()); err != nil {
		return err
	}
	if err := s.stakingEvent(ctx, tx, pos.UID, &pos.ID, pos.ProductID, "release", pos.Asset, pos.Amount,
		map[string]any{"rewards": rewards}); err != nil {
		return err
	}
	return nil
}

var _ = errors.New
