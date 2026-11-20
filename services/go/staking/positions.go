package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Position lifecycle:
//   active     — earning daily rewards until the user unstakes.
//   unbonding  — unstake requested; principal released at unbond_at.
//   released   — principal returned to spot; terminal.
//   cancelled  — stake failed to settle (compensation path); terminal.

type position struct {
	ID              int64            `json:"id"`
	UID             string           `json:"-"`
	ProductID       string           `json:"productId"`
	Asset           string           `json:"asset"`
	Amount          decimal.Decimal  `json:"amount"`
	APRSnapshot     decimal.Decimal  `json:"apr"`
	Status          string           `json:"status"`
	StartedAt       time.Time        `json:"-"`
	UnbondAt        *time.Time       `json:"-"`
	ReleasedAt      *time.Time       `json:"-"`
	LastSettleDay   int              `json:"-"`
	PendingRewards  decimal.Decimal  `json:"pendingRewards"`
	ClaimedRewards  decimal.Decimal  `json:"claimedRewards"`
	Validator       string           `json:"validator,omitempty"`
	Chain           string           `json:"chain,omitempty"`
	UnbondingDays   int              `json:"unbondingDays,omitempty"`
	StartedAtMillis int64            `json:"startedAt"`
	UnbondAtMillis  *int64           `json:"unbondAt,omitempty"`
	ReleasedMillis  *int64           `json:"releasedAt,omitempty"`
	EstDaily        decimal.Decimal  `json:"estDailyReward"`
}

const positionCols = `id, uid, product_id, asset, amount, apr_snapshot, status, started_at, unbond_at, released_at,
	last_settle_day, pending_rewards, claimed_rewards`

func scanPosition(sc scanner) (*position, error) {
	p := &position{}
	err := sc.Scan(&p.ID, &p.UID, &p.ProductID, &p.Asset, &p.Amount, &p.APRSnapshot, &p.Status,
		&p.StartedAt, &p.UnbondAt, &p.ReleasedAt, &p.LastSettleDay, &p.PendingRewards, &p.ClaimedRewards)
	if err == nil {
		p.StartedAtMillis = p.StartedAt.UnixMilli()
		if p.UnbondAt != nil {
			v := p.UnbondAt.UnixMilli()
			p.UnbondAtMillis = &v
		}
		if p.ReleasedAt != nil {
			v := p.ReleasedAt.UnixMilli()
			p.ReleasedMillis = &v
		}
	}
	return p, err
}

var days365 = decimal.NewFromInt(365)

// dailyReward is one day of reward on principal at the position's locked APR.
func dailyReward(principal, apr decimal.Decimal) decimal.Decimal {
	if principal.Sign() <= 0 || apr.Sign() <= 0 {
		return decimal.Zero
	}
	return floor8(principal.Mul(apr).Div(days365))
}

func parseAmount(raw string) (decimal.Decimal, error) {
	a, err := decimal.NewFromString(raw)
	if err != nil || a.Sign() <= 0 || a.Exponent() < -dp {
		return decimal.Zero, bad("VALIDATION_ERROR", "数量不合法")
	}
	return a, nil
}

func (s *Server) stakingEvent(ctx context.Context, tx *sql.Tx, uid string, positionID *int64, productID, kind, asset string, amount decimal.Decimal, detail map[string]any) error {
	d := []byte("{}")
	if detail != nil {
		d, _ = json.Marshal(detail)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO s_events (uid, position_id, product_id, kind, asset, amount, detail, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, uid, positionID, productID, kind, asset, amount, d, s.now())
	return err
}

// ---- stake ----

type stakeReq struct {
	Asset     string `json:"asset"`
	Validator string `json:"validator"` // either a product id or the validator's display name
	Amount    string `json:"amount"`
}

func (s *Server) handleStake(w http.ResponseWriter, r *http.Request, uid string) {
	var req stakeReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.stake(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// resolveProduct picks the product by id or (asset, validator). The validator
// field accepts an id, an exact display name or a case-insensitive match.
func (s *Server) resolveProduct(ctx context.Context, tx *sql.Tx, asset, validator string) (*product, error) {
	if validator == "" {
		return nil, bad("VALIDATION_ERROR", "validator 不能为空")
	}
	// Try id match first — this is the stable identifier Flutter can save.
	if p, err := scanProduct(tx.QueryRowContext(ctx, `SELECT `+productCols+` FROM s_products WHERE id=$1 FOR UPDATE`, validator)); err == nil {
		if asset != "" && !strings.EqualFold(p.Asset, asset) {
			return nil, bad("VALIDATION_ERROR", "asset 与 validator 不匹配")
		}
		return p, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if asset == "" {
		return nil, bad("VALIDATION_ERROR", "asset 不能为空")
	}
	a := strings.ToUpper(asset)
	p, err := scanProduct(tx.QueryRowContext(ctx, `SELECT `+productCols+` FROM s_products
		WHERE asset=$1 AND lower(validator)=lower($2) FOR UPDATE`, a, validator))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("PRODUCT_NOT_FOUND", "该币种/验证节点暂不支持质押")
	}
	return p, err
}

func (s *Server) stake(ctx context.Context, uid string, req stakeReq) (map[string]any, error) {
	amount, err := parseAmount(req.Amount)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := s.resolveProduct(ctx, tx, req.Asset, req.Validator)
	if err != nil {
		return nil, err
	}
	if p.Status != "online" {
		return nil, conflict("PRODUCT_OFFLINE", "产品已下架")
	}
	if amount.LessThan(p.MinAmount) {
		return nil, bad("AMOUNT_TOO_SMALL", "最低质押 "+p.MinAmount.String()+" "+p.Asset)
	}
	if p.TotalQuota != nil && p.Sold.Add(amount).GreaterThan(*p.TotalQuota) {
		return nil, conflict("QUOTA_EXCEEDED", "产品剩余额度不足")
	}
	if p.MaxAmount != nil {
		var open decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM s_positions
			WHERE uid=$1 AND product_id=$2 AND status IN ('active','unbonding')`, uid, p.ID).Scan(&open); err != nil {
			return nil, err
		}
		if open.Add(amount).GreaterThan(*p.MaxAmount) {
			return nil, bad("USER_LIMIT_EXCEEDED", "超过个人持仓上限 "+p.MaxAmount.String()+" "+p.Asset)
		}
	}
	now := s.now()
	today := s.dayIndex(now)
	// Rewards start accruing from the next UTC day: today's partial day is
	// excluded on both sides so settlement is symmetric.
	var pid int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO s_positions
		(uid, product_id, asset, amount, apr_snapshot, status, started_at, last_settle_day)
		VALUES ($1,$2,$3,$4,$5,'active',$6,$7) RETURNING id`,
		uid, p.ID, p.Asset, amount, p.APR, now, today).Scan(&pid); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE s_products SET sold=sold+$2 WHERE id=$1`, p.ID, amount); err != nil {
		return nil, err
	}
	if err := s.stakingEvent(ctx, tx, uid, &pid, p.ID, "stake", p.Asset, amount, map[string]any{
		"apr": p.APR, "validator": p.Validator, "unbondingDays": p.UnbondingDays,
	}); err != nil {
		return nil, err
	}
	ref := fmt.Sprintf("staking:stake:%d", pid)
	if err := s.systemTransfer(ctx, uid, p.Asset, "staking", "debit", amount, ref); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		// Transfer succeeded but the position write did not: compensate.
		_ = s.systemTransfer(context.Background(), uid, p.Asset, "staking", "credit", amount, ref+":revert")
		return nil, err
	}
	return map[string]any{
		"positionId":      pid,
		"productId":       p.ID,
		"asset":           p.Asset,
		"validator":       p.Validator,
		"amount":          amount,
		"apr":             p.APR,
		"unbondingDays":   p.UnbondingDays,
		"rewardsStartAt":  time.Unix(int64(today+1)*86400, 0).UnixMilli(),
	}, nil
}

// ---- unstake ----

type unstakeReq struct {
	PositionID int64  `json:"positionId"`
	Amount     string `json:"amount"` // optional; omit or 0 = full
}

func (s *Server) handleUnstake(w http.ResponseWriter, r *http.Request, uid string) {
	var req unstakeReq
	if err := common.DecodeJSON(r, &req); err != nil || req.PositionID <= 0 {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.unstake(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) unstake(ctx context.Context, uid string, req unstakeReq) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	pos, err := s.lockPosition(ctx, tx, req.PositionID)
	if err != nil {
		return nil, err
	}
	if pos.UID != uid {
		return nil, notFound("POSITION_NOT_FOUND", "质押仓位不存在")
	}
	if pos.Status != "active" {
		return nil, conflict("POSITION_NOT_ACTIVE", "仓位当前不可解质押")
	}
	prod, err := s.loadProduct(ctx, tx, pos.ProductID, true)
	if err != nil {
		return nil, err
	}
	// Settle any whole-day rewards earned up to today before we touch the
	// amount: pending_rewards must already include everything owed.
	if err := s.settlePosition(ctx, tx, pos); err != nil {
		return nil, err
	}
	amount := pos.Amount
	if strings.TrimSpace(req.Amount) != "" && req.Amount != "0" {
		amount, err = parseAmount(req.Amount)
		if err != nil {
			return nil, err
		}
		if amount.GreaterThan(pos.Amount) {
			return nil, bad("AMOUNT_TOO_LARGE", "解质押数量超过仓位")
		}
	}
	partial := amount.LessThan(pos.Amount)

	// Split the position when partial: the untouched remainder keeps earning
	// under a fresh row so pending_rewards and unbond_at stay one-to-one.
	var leaving *position
	if partial {
		remain := pos.Amount.Sub(amount)
		// Pro-rata the pending rewards: the leaving share takes its slice and
		// is claimable on release; the remainder keeps its slice earning.
		ratio := amount.Div(pos.Amount)
		pendingLeave := floor8(pos.PendingRewards.Mul(ratio))
		pendingStay := pos.PendingRewards.Sub(pendingLeave)
		// Shrink the original row to the remainder.
		if _, err := tx.ExecContext(ctx, `UPDATE s_positions SET amount=$2, pending_rewards=$3 WHERE id=$1`,
			pos.ID, remain, pendingStay); err != nil {
			return nil, err
		}
		// Insert the leaving share as its own position; its day-rewards past
		// this point are irrelevant (status moves straight to unbonding).
		var nid int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO s_positions
			(uid, product_id, asset, amount, apr_snapshot, status, started_at, last_settle_day, pending_rewards)
			VALUES ($1,$2,$3,$4,$5,'active',$6,$7,$8) RETURNING id`,
			uid, pos.ProductID, pos.Asset, amount, pos.APRSnapshot, pos.StartedAt, s.dayIndex(s.now()), pendingLeave).Scan(&nid); err != nil {
			return nil, err
		}
		leaving, err = s.lockPosition(ctx, tx, nid)
		if err != nil {
			return nil, err
		}
	} else {
		leaving = pos
	}

	now := s.now()
	out := map[string]any{"positionId": leaving.ID, "asset": pos.Asset, "amount": amount, "status": "unbonding"}
	if prod.UnbondingDays == 0 {
		// Instant release path (e.g. ADA).
		if err := s.releasePosition(ctx, tx, leaving); err != nil {
			return nil, err
		}
		out["status"] = "released"
	} else {
		rel := now.Add(time.Duration(prod.UnbondingDays) * 24 * time.Hour)
		if _, err := tx.ExecContext(ctx, `UPDATE s_positions SET status='unbonding', unbond_at=$2 WHERE id=$1`, leaving.ID, rel); err != nil {
			return nil, err
		}
		if err := s.stakingEvent(ctx, tx, uid, &leaving.ID, pos.ProductID, "unstake", pos.Asset, amount,
			map[string]any{"unbondAt": rel.UnixMilli(), "unbondingDays": prod.UnbondingDays}); err != nil {
			return nil, err
		}
		out["unbondAt"] = rel.UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE s_products SET sold=GREATEST(sold-$2,0) WHERE id=$1`, pos.ProductID, amount); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// lockPosition selects FOR UPDATE and returns the fresh row.
func (s *Server) lockPosition(ctx context.Context, tx *sql.Tx, id int64) (*position, error) {
	p, err := scanPosition(tx.QueryRowContext(ctx, `SELECT `+positionCols+` FROM s_positions WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("POSITION_NOT_FOUND", "质押仓位不存在")
	}
	return p, err
}

// ---- claim ----

type claimReq struct {
	PositionID int64 `json:"positionId"` // 0 = claim across all active positions
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request, uid string) {
	var req claimReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.claim(r.Context(), uid, req.PositionID)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// claim pays out pending_rewards on one position or every active position.
// Rewards still accrue on principal afterwards; the claim only moves the
// already-settled slice from the staking_rewards ledger onto the spot ledger.
func (s *Server) claim(ctx context.Context, uid string, positionID int64) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var ids []int64
	if positionID > 0 {
		pos, err := s.lockPosition(ctx, tx, positionID)
		if err != nil {
			return nil, err
		}
		if pos.UID != uid {
			return nil, notFound("POSITION_NOT_FOUND", "质押仓位不存在")
		}
		ids = []int64{pos.ID}
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM s_positions WHERE uid=$1 AND status IN ('active','unbonding')
			AND pending_rewards > 0 ORDER BY id FOR UPDATE`, uid)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		rows.Close()
	}

	type perAsset struct {
		asset  string
		amount decimal.Decimal
	}
	byAsset := map[string]*perAsset{}
	type claimedRow struct {
		positionID int64
		amount     decimal.Decimal
	}
	var claimed []claimedRow
	now := s.now()
	for _, id := range ids {
		pos, err := scanPosition(tx.QueryRowContext(ctx, `SELECT `+positionCols+` FROM s_positions WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return nil, err
		}
		// Make sure every finished day is in pending_rewards before we pay out.
		if err := s.settlePosition(ctx, tx, pos); err != nil {
			return nil, err
		}
		if pos.PendingRewards.Sign() <= 0 {
			continue
		}
		amt := pos.PendingRewards
		// Mark the underlying reward rows claimed (auditability).
		if _, err := tx.ExecContext(ctx, `UPDATE s_rewards SET claimed_at=$2 WHERE position_id=$1 AND claimed_at IS NULL`, pos.ID, now); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE s_positions SET pending_rewards=0, claimed_rewards=claimed_rewards+$2 WHERE id=$1`, pos.ID, amt); err != nil {
			return nil, err
		}
		if err := s.stakingEvent(ctx, tx, uid, &pos.ID, pos.ProductID, "claim", pos.Asset, amt, nil); err != nil {
			return nil, err
		}
		pa, ok := byAsset[pos.Asset]
		if !ok {
			pa = &perAsset{asset: pos.Asset}
			byAsset[pos.Asset] = pa
		}
		pa.amount = pa.amount.Add(amt)
		claimed = append(claimed, claimedRow{positionID: pos.ID, amount: amt})
	}

	// Credit each asset's total to the spot ledger. Determinist ref: the max
	// claimed row id suffices because claim marks every included reward row.
	for _, pa := range byAsset {
		if pa.amount.Sign() <= 0 {
			continue
		}
		ref := fmt.Sprintf("staking:claim:%s:%s:%d", uid, pa.asset, now.UnixNano())
		if err := s.systemTransfer(ctx, uid, pa.asset, "staking_rewards", "credit", pa.amount, ref); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(claimed))
	for _, c := range claimed {
		items = append(items, map[string]any{"positionId": c.positionID, "amount": c.amount})
	}
	totals := make([]map[string]any, 0, len(byAsset))
	for _, pa := range byAsset {
		totals = append(totals, map[string]any{"asset": pa.asset, "amount": pa.amount})
	}
	return map[string]any{"items": items, "totals": totals}, nil
}

// ---- reads ----

func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request, uid string) {
	status := r.URL.Query().Get("status") // active|unbonding|released or empty = non-terminal
	var (
		rows *sql.Rows
		err  error
	)
	base := `SELECT p.id, p.uid, p.product_id, p.asset, p.amount, p.apr_snapshot, p.status, p.started_at, p.unbond_at,
			p.released_at, p.last_settle_day, p.pending_rewards, p.claimed_rewards, pr.validator, pr.chain, pr.unbonding_days
		FROM s_positions p JOIN s_products pr ON pr.id=p.product_id WHERE p.uid=$1`
	if status != "" {
		rows, err = s.db.QueryContext(r.Context(), base+` AND p.status=$2 ORDER BY p.id DESC LIMIT 500`, uid, status)
	} else {
		rows, err = s.db.QueryContext(r.Context(), base+` AND p.status IN ('active','unbonding') ORDER BY p.id DESC LIMIT 500`, uid)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*position{}
	for rows.Next() {
		p := &position{}
		if err := rows.Scan(&p.ID, &p.UID, &p.ProductID, &p.Asset, &p.Amount, &p.APRSnapshot, &p.Status,
			&p.StartedAt, &p.UnbondAt, &p.ReleasedAt, &p.LastSettleDay, &p.PendingRewards, &p.ClaimedRewards,
			&p.Validator, &p.Chain, &p.UnbondingDays); err != nil {
			writeErr(w, err)
			return
		}
		p.StartedAtMillis = p.StartedAt.UnixMilli()
		if p.UnbondAt != nil {
			v := p.UnbondAt.UnixMilli()
			p.UnbondAtMillis = &v
		}
		if p.ReleasedAt != nil {
			v := p.ReleasedAt.UnixMilli()
			p.ReleasedMillis = &v
		}
		p.EstDaily = dailyReward(p.Amount, p.APRSnapshot)
		items = append(items, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRewards(w http.ResponseWriter, r *http.Request, uid string) {
	q := r.URL.Query()
	where := []string{"uid=$1"}
	args := []any{uid}
	if a := strings.ToUpper(q.Get("asset")); a != "" {
		args = append(args, a)
		where = append(where, fmt.Sprintf("asset=$%d", len(args)))
	}
	if pid := q.Get("positionId"); pid != "" {
		id, err := strconv.ParseInt(pid, 10, 64)
		if err != nil {
			writeErr(w, bad("VALIDATION_ERROR", "positionId 不合法"))
			return
		}
		args = append(args, id)
		where = append(where, fmt.Sprintf("position_id=$%d", len(args)))
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, position_id, asset, epoch, amount, created_at, claimed_at
		FROM s_rewards WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT 500`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, pid int64
		var asset string
		var epoch int
		var amt decimal.Decimal
		var created time.Time
		var claimed sql.NullTime
		if err := rows.Scan(&id, &pid, &asset, &epoch, &amt, &created, &claimed); err != nil {
			writeErr(w, err)
			return
		}
		m := map[string]any{"id": id, "positionId": pid, "asset": asset, "epoch": epoch, "amount": amt, "createdAt": created.UnixMilli()}
		if claimed.Valid {
			m["claimedAt"] = claimed.Time.UnixMilli()
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, uid string) {
	q := r.URL.Query()
	where := []string{"uid=$1"}
	args := []any{uid}
	if k := q.Get("kind"); k != "" {
		args = append(args, k)
		where = append(where, fmt.Sprintf("kind=$%d", len(args)))
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, position_id, product_id, kind, asset, amount, detail, at
		FROM s_events WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT 500`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var pid sql.NullInt64
		var product sql.NullString
		var kind, asset string
		var amt decimal.Decimal
		var detail []byte
		var at time.Time
		if err := rows.Scan(&id, &pid, &product, &kind, &asset, &amt, &detail, &at); err != nil {
			writeErr(w, err)
			return
		}
		m := map[string]any{"id": id, "kind": kind, "asset": asset, "amount": amt, "detail": json.RawMessage(detail), "at": at.UnixMilli()}
		if pid.Valid {
			m["positionId"] = pid.Int64
		}
		if product.Valid {
			m["productId"] = product.String
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleSummary aggregates per-asset staked amount and rewards.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT asset,
		COALESCE(SUM(CASE WHEN status='active' THEN amount ELSE 0 END),0) AS staked,
		COALESCE(SUM(CASE WHEN status='unbonding' THEN amount ELSE 0 END),0) AS unbonding,
		COALESCE(SUM(pending_rewards),0) AS pending,
		COALESCE(SUM(claimed_rewards),0) AS claimed
		FROM s_positions WHERE uid=$1 AND status IN ('active','unbonding') GROUP BY asset ORDER BY asset`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var asset string
		var staked, unbond, pending, claimed decimal.Decimal
		if err := rows.Scan(&asset, &staked, &unbond, &pending, &claimed); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{
			"asset":           asset,
			"staked":          staked,
			"unbonding":       unbond,
			"pendingRewards":  pending,
			"claimedRewards":  claimed,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"assets": items})
}
