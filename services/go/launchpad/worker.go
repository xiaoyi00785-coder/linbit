package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/shopspring/decimal"
)

// openWindows: flip 'upcoming' rows whose start_at has arrived to 'open'. This
// is cosmetic — subscribe/cancel already gate by wall-clock — but keeps the
// admin / list views honest even between Ticks.
func (s *Server) openWindows(ctx context.Context) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `UPDATE launchpad_projects SET status='open'
		WHERE status='upcoming' AND start_at <= $1 AND end_at > $1`, now)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE launchpad_projects SET status='computing'
		WHERE status IN ('upcoming','open') AND end_at <= $1 AND settle_at > $1`, now)
	return err
}

// takeSnapshots writes one (uid, project, hour) row per live subscription for
// the current hour bucket.  Unique constraint makes this idempotent, so a
// Ticker firing more than once per hour is safe.
func (s *Server) takeSnapshots(ctx context.Context) error {
	now := s.now()
	hour := s.hourBucket(now)
	rows, err := s.db.QueryContext(ctx, `SELECT s.uid, s.project_id, s.amount, p.end_at, p.start_at
		FROM launchpad_subscriptions s JOIN launchpad_projects p ON p.id = s.project_id
		WHERE s.status='holding' AND s.amount > 0 AND p.status <> 'cancelled' AND p.start_at <= $1 AND p.end_at > $1`, now)
	if err != nil {
		return err
	}
	type sn struct {
		uid, pid string
		amount   decimal.Decimal
	}
	var snaps []sn
	for rows.Next() {
		var x sn
		var _end, _start time.Time
		if err := rows.Scan(&x.uid, &x.pid, &x.amount, &_end, &_start); err != nil {
			rows.Close()
			return err
		}
		snaps = append(snaps, x)
	}
	rows.Close()
	for _, x := range snaps {
		_, err := s.db.ExecContext(ctx, `INSERT INTO launchpad_snapshots (uid, project_id, hour_bucket, base_balance, taken_at)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT (uid, project_id, hour_bucket) DO NOTHING`,
			x.uid, x.pid, hour, x.amount, now)
		if err != nil {
			log.Printf("launchpad: snapshot %s/%s: %v", x.uid, x.pid, err)
		}
	}
	return nil
}

// settleDue: for every project whose settle_at has passed and whose status is
// still pre-settled, compute allocations and write per-subscription results.
// Funds do not move at settle; distribute does that.
func (s *Server) settleDue(ctx context.Context) error {
	now := s.now()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM launchpad_projects
		WHERE status IN ('upcoming','open','computing') AND settle_at <= $1 ORDER BY settle_at`, now)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.settleOne(ctx, id); err != nil {
			log.Printf("launchpad: settle %s: %v", id, err)
		}
	}
	return nil
}

// settleOne is the oversubscription calculator. For every active subscription
// we take the AVERAGE of its hourly snapshots (if there is at least one) or
// its current locked amount (window too short / no snapshots) as the base. The
// pro-rata allocation is:
//
//	totalRaiseCap = totalSupply * price
//	totalAvg      = Σ user_avg
//	ratio         = min(1, totalRaiseCap / totalAvg)   // > 1 would exceed cap
//	user_used     = min(user_avg * ratio, user_locked) // never exceed lock
//	user_tokens   = user_used / price                   // floored to dp
//	user_refund   = user_locked - user_used
//
// Everything is deterministic. The result is persisted so a re-settle is a no-op.
func (s *Server) settleOne(ctx context.Context, pid string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.loadProject(ctx, tx, pid, true)
	if err != nil {
		return err
	}
	if p.Status == "settled" || p.Status == "distributed" {
		return nil
	}
	if p.Status == "cancelled" {
		return nil
	}
	// Gather every live subscription (holding w/ amount > 0) for this project.
	rows, err := tx.QueryContext(ctx, `SELECT id, uid, amount FROM launchpad_subscriptions
		WHERE project_id=$1 AND status='holding' AND amount > 0 FOR UPDATE`, pid)
	if err != nil {
		return err
	}
	type entry struct {
		id     int64
		uid    string
		locked decimal.Decimal
		avg    decimal.Decimal
	}
	var subs []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.uid, &e.locked); err != nil {
			rows.Close()
			return err
		}
		subs = append(subs, e)
	}
	rows.Close()
	totalAvg := decimal.Zero
	for i := range subs {
		var avg sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT AVG(base_balance)::text FROM launchpad_snapshots
			WHERE uid=$1 AND project_id=$2`, subs[i].uid, pid).Scan(&avg); err != nil {
			return err
		}
		if avg.Valid && avg.String != "" {
			d, err := decimal.NewFromString(avg.String)
			if err != nil {
				return err
			}
			subs[i].avg = d
		} else {
			subs[i].avg = subs[i].locked
		}
		totalAvg = totalAvg.Add(subs[i].avg)
	}
	cap := p.TotalSupply.Mul(p.Price)
	oversub := totalAvg.Sign() > 0 && cap.LessThan(totalAvg)
	var usedTotal, tokensTotal decimal.Decimal
	now := s.now()
	for _, e := range subs {
		// When oversubscribed, used = avg * cap / totalAvg — multiply before
		// divide so 150 * 100 / 300 stays exactly 50 instead of compounding
		// the rounding error from a separate ratio.Div() step.
		used := e.avg
		if oversub {
			used = floor8(e.avg.Mul(cap).Div(totalAvg))
		}
		if used.GreaterThan(e.locked) {
			used = e.locked
		}
		tokens := floor8(used.Div(p.Price))
		refund := e.locked.Sub(used)
		if refund.Sign() < 0 {
			refund = decimal.Zero
		}
		if _, err := tx.ExecContext(ctx, `UPDATE launchpad_subscriptions
			SET avg_base=$2, result_amount=$3, used_base=$4, refund_amount=$5, status='settled', settled_at=$6
			WHERE id=$1`, e.id, e.avg, tokens, used, refund, now); err != nil {
			return err
		}
		if err := s.event(ctx, tx, e.uid, pid, "settle", p.Symbol, tokens, map[string]any{
			"avgBase": e.avg, "usedBase": used, "refund": refund, "oversubscribed": oversub,
		}); err != nil {
			return err
		}
		usedTotal = usedTotal.Add(used)
		tokensTotal = tokensTotal.Add(tokens)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE launchpad_projects
		SET status='settled', allocated=$2, raised=$3, settled_at=$4 WHERE id=$1`,
		pid, tokensTotal, usedTotal, now); err != nil {
		return err
	}
	return tx.Commit()
}

// distributeDue runs on every Tick after settle_at + distribute_at.
func (s *Server) distributeDue(ctx context.Context) error {
	now := s.now()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM launchpad_projects
		WHERE status='settled' AND distribute_at <= $1 ORDER BY distribute_at`, now)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.distributeOne(ctx, id); err != nil {
			log.Printf("launchpad: distribute %s: %v", id, err)
		}
	}
	return nil
}

// distributeOne credits the new token and refund to each settled subscription.
// Both transfers use deterministic refs so a crash-retry (or admin re-run) is
// idempotent at the asset ledger. The project row is flipped only when every
// subscription has been credited.
func (s *Server) distributeOne(ctx context.Context, pid string) error {
	// Load the project once, read-only, to learn its symbol/base.
	p, err := s.loadProject(ctx, s.db, pid, false)
	if err != nil {
		return err
	}
	if p.Status != "settled" && p.Status != "distributed" {
		return fmt.Errorf("project %s not in settled state (status=%s)", pid, p.Status)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, uid, result_amount, used_base, refund_amount, amount
		FROM launchpad_subscriptions WHERE project_id=$1 AND status='settled' ORDER BY id`, pid)
	if err != nil {
		return err
	}
	type payout struct {
		id     int64
		uid    string
		tokens decimal.Decimal
		refund decimal.Decimal
	}
	var list []payout
	for rows.Next() {
		var x payout
		var used, locked decimal.Decimal
		if err := rows.Scan(&x.id, &x.uid, &x.tokens, &used, &x.refund, &locked); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		if err := s.payOne(ctx, p, x.id, x.uid, x.tokens, x.refund); err != nil {
			log.Printf("launchpad: payout sub %d: %v", x.id, err)
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE launchpad_projects
		SET status='distributed', distributed_at=$2 WHERE id=$1`, pid, s.now())
	return err
}

func (s *Server) payOne(ctx context.Context, p *project, sid int64, uid string, tokens, refund decimal.Decimal) error {
	if tokens.Sign() > 0 {
		ref := fmt.Sprintf("launchpad:win:%d", sid)
		if err := s.systemTransfer(ctx, uid, p.Symbol, "credit", tokens, ref); err != nil {
			return err
		}
	}
	if refund.Sign() > 0 {
		ref := fmt.Sprintf("launchpad:refund:%d", sid)
		if err := s.systemTransfer(ctx, uid, p.BaseAsset, "credit", refund, ref); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE launchpad_subscriptions SET status='distributed', distributed_at=$2
		WHERE id=$1 AND status='settled'`, sid, s.now()); err != nil {
		return err
	}
	if tokens.Sign() > 0 {
		if err := s.event(ctx, tx, uid, p.ID, "distribute", p.Symbol, tokens, nil); err != nil {
			return err
		}
	}
	if refund.Sign() > 0 {
		if err := s.event(ctx, tx, uid, p.ID, "refund", p.BaseAsset, refund, nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// cancelProject refunds every live subscription and marks the project
// cancelled.  Admin-only path, used when a project is pulled before settlement
// (e.g. compliance veto).
func (s *Server) cancelProject(ctx context.Context, pid string) error {
	p, err := s.loadProject(ctx, s.db, pid, false)
	if err != nil {
		return err
	}
	if p.Status == "distributed" {
		return conflict("ALREADY_DISTRIBUTED", "已分发，不可取消")
	}
	if p.Status == "cancelled" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, uid, amount FROM launchpad_subscriptions
		WHERE project_id=$1 AND status IN ('holding','settled') ORDER BY id`, pid)
	if err != nil {
		return err
	}
	type entry struct {
		id     int64
		uid    string
		amount decimal.Decimal
	}
	var subs []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.uid, &e.amount); err != nil {
			rows.Close()
			return err
		}
		subs = append(subs, e)
	}
	rows.Close()
	for _, e := range subs {
		if e.amount.Sign() > 0 {
			ref := fmt.Sprintf("launchpad:adm-cancel:%d", e.id)
			if err := s.systemTransfer(ctx, e.uid, p.BaseAsset, "credit", e.amount, ref); err != nil {
				return err
			}
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE launchpad_subscriptions
			SET status='cancelled', refund_amount=$2, amount=0 WHERE id=$1`, e.id, e.amount); err != nil {
			tx.Rollback()
			return err
		}
		if err := s.event(ctx, tx, e.uid, pid, "cancel", p.BaseAsset, e.amount, map[string]any{"adminCancel": true}); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE launchpad_projects SET status='cancelled' WHERE id=$1`, pid)
	return err
}
