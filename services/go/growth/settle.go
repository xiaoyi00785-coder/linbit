package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// settle.go computes partner / referral commissions from unsettled fee events
// and pays them into the funding account once per day.
//
// Rules:
//   - Rebate base = fee - trial-bonus-paid fee - fee-coupon-covered fee (USDT).
//   - If the trader's inviter is an ACTIVE partner, the partner earns
//     base x tier rate (futures / spot). Otherwise the inviter earns the
//     regular referral rate for ReferralPeriod after the invitee registered.
//     The two never stack; only one level of invitees counts.
//   - Override: the partner's own inviter, if an active partner, receives
//     OverrideRate x the direct commission (platform-funded, not deducted
//     from the sub-partner), up to OverrideDepth levels.
//   - Copy-trading profit share is not a fee and never enters this pipeline.

type commKey struct {
	Ben, Src, Kind, Day string
}

type commRow struct {
	commKey
	Tier   string
	Rate   decimal.Decimal
	Base   decimal.Decimal
	Amount decimal.Decimal
}

type queryer interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

func (s *Server) loadPartners(ctx context.Context, q queryer) (map[string]string, map[string]string, error) {
	tiers := map[string]string{}
	rows, err := q.QueryContext(ctx, `SELECT uid, tier FROM g_partners WHERE status='active'`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var u, t string
		if err := rows.Scan(&u, &t); err != nil {
			rows.Close()
			return nil, nil, err
		}
		tiers[u] = t
	}
	rows.Close()
	parents := map[string]string{}
	rows, err = q.QueryContext(ctx, `SELECT u.uid, u.inviter FROM g_users u JOIN g_partners p ON p.uid=u.uid
		WHERE p.status='active' AND u.inviter IS NOT NULL`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u, p string
		if err := rows.Scan(&u, &p); err != nil {
			return nil, nil, err
		}
		parents[u] = p
	}
	return tiers, parents, rows.Err()
}

// computeCommissions evaluates unsettled events before cutoff. When onlyBen is
// set, only rows for that beneficiary are returned (used for previews).
func (s *Server) computeCommissions(ctx context.Context, q queryer, cutoff time.Time, onlyBen string) ([]*commRow, error) {
	tiers, parents, err := s.loadPartners(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT e.uid, e.product, GREATEST(e.fee - e.bonus_fee - e.coupon_fee, 0), e.at, u.inviter, u.registered_at
		FROM g_fee_events e JOIN g_users u ON u.uid=e.uid
		WHERE e.settlement_id IS NULL AND e.at < $1 AND u.inviter IS NOT NULL AND e.fee > 0`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agg := map[commKey]*commRow{}
	add := func(k commKey, tier string, rate, base decimal.Decimal) decimal.Decimal {
		amt := base.Mul(rate)
		if onlyBen != "" && k.Ben != onlyBen {
			return amt
		}
		r := agg[k]
		if r == nil {
			r = &commRow{commKey: k, Tier: tier, Rate: rate}
			agg[k] = r
		}
		r.Base = r.Base.Add(base)
		r.Amount = r.Amount.Add(amt)
		return amt
	}
	for rows.Next() {
		var uid, product, inviter string
		var base decimal.Decimal
		var at, reg time.Time
		if err := rows.Scan(&uid, &product, &base, &at, &inviter, &reg); err != nil {
			return nil, err
		}
		if base.Sign() <= 0 {
			continue
		}
		day := s.day(at)
		if tk, ok := tiers[inviter]; ok {
			t := s.cfg.tier(tk)
			rate := t.SpotRate
			if product == "futures" {
				rate = t.FuturesRate
			}
			direct := add(commKey{inviter, uid, product, day}, t.Key, rate, base)
			sub := inviter
			for d := 0; d < s.cfg.OverrideDepth; d++ {
				p, ok := parents[sub]
				if !ok {
					break
				}
				if _, active := tiers[p]; !active {
					break
				}
				add(commKey{p, sub, "override", day}, tiers[p], s.cfg.OverrideRate, direct)
				sub = p
			}
			continue
		}
		if at.Before(reg.Add(s.cfg.ReferralPeriod)) {
			add(commKey{inviter, uid, "referral", day}, "", s.cfg.ReferralRate, base)
		}
	}
	out := make([]*commRow, 0, len(agg))
	for _, r := range agg {
		out = append(out, r)
	}
	return out, rows.Err()
}

// updateTiers recomputes every active partner's tier from the futures
// notional traded by their direct invitees in [cutoff - TierWindow, cutoff).
func (s *Server) updateTiers(ctx context.Context, tx *sql.Tx, cutoff time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT p.uid, COALESCE((SELECT SUM(e.notional) FROM g_fee_events e JOIN g_users u ON u.uid=e.uid
		WHERE u.inviter=p.uid AND e.product='futures' AND e.at >= $1 AND e.at < $2),0)
		FROM g_partners p WHERE p.status='active'`, cutoff.Add(-s.cfg.TierWindow), cutoff)
	if err != nil {
		return err
	}
	type pv struct {
		uid string
		vol decimal.Decimal
	}
	var list []pv
	for rows.Next() {
		var x pv
		if err := rows.Scan(&x.uid, &x.vol); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		if _, err := tx.ExecContext(ctx, `UPDATE g_partners SET tier=$2, volume30d=$3 WHERE uid=$1`, x.uid, s.cfg.tierFor(x.vol).Key, x.vol); err != nil {
			return err
		}
	}
	return nil
}

// Settle closes all events before cutoff into one settlement. Returns 0 when
// a settlement at or after cutoff already exists.
func (s *Server) Settle(ctx context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM g_settlements WHERE cutoff >= $1)`, cutoff).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, nil
	}
	if err := s.updateTiers(ctx, tx, cutoff); err != nil {
		return 0, err
	}
	var sid int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO g_settlements (cutoff) VALUES ($1) RETURNING id`, cutoff).Scan(&sid); err != nil {
		return 0, err
	}
	comms, err := s.computeCommissions(ctx, tx, cutoff, "")
	if err != nil {
		return 0, err
	}
	totals := map[string]decimal.Decimal{}
	for _, c := range comms {
		if _, err := tx.ExecContext(ctx, `INSERT INTO g_commissions (settlement_id, beneficiary, source_uid, kind, tier, rate, base, amount, day)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, sid, c.Ben, c.Src, c.Kind, c.Tier, c.Rate, c.Base.Round(12), c.Amount.Round(12), c.Day); err != nil {
			return 0, err
		}
		totals[c.Ben] = totals[c.Ben].Add(c.Amount)
	}
	res, err := tx.ExecContext(ctx, `UPDATE g_fee_events SET settlement_id=$1 WHERE settlement_id IS NULL AND at < $2`, sid, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `UPDATE g_settlements SET events=$2 WHERE id=$1`, sid, n); err != nil {
		return 0, err
	}
	for uid, amt := range totals {
		amt = trunc(amt, 8)
		if amt.Sign() <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO g_payouts (settlement_id, uid, amount, ref, status) VALUES ($1,$2,$3,$4,'pending')`,
			sid, uid, amt, fmt.Sprintf("growth_comm_%d_%s", sid, uid)); err != nil {
			return 0, err
		}
	}
	return sid, tx.Commit()
}

// PayPending credits pending payouts from the platform rebate account.
func (s *Server) PayPending(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT settlement_id, uid, amount, ref FROM g_payouts WHERE status='pending' ORDER BY settlement_id LIMIT 200`)
	if err != nil {
		return 0, err
	}
	type po struct {
		sid      int64
		uid, ref string
		amt      decimal.Decimal
	}
	var list []po
	for rows.Next() {
		var p po
		if err := rows.Scan(&p.sid, &p.uid, &p.amt, &p.ref); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, p)
	}
	rows.Close()
	paid := 0
	for _, p := range list {
		if err := s.systemTransfer(ctx, p.uid, s.cfg.PayoutAsset, "rebate", "credit", p.amt, p.ref); err != nil {
			return paid, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE g_payouts SET status='paid', paid_at=now() WHERE settlement_id=$1 AND uid=$2`, p.sid, p.uid); err != nil {
			return paid, err
		}
		paid++
	}
	return paid, nil
}
