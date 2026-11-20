package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

func floor8(d decimal.Decimal) decimal.Decimal { return d.RoundFloor(dp) }

func parseAmount(raw string) (decimal.Decimal, error) {
	a, err := decimal.NewFromString(raw)
	if err != nil || a.Sign() <= 0 || a.Exponent() < -dp {
		return decimal.Zero, bad("VALIDATION_ERROR", "数量不合法")
	}
	return a, nil
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Amount string `json:"amount"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.subscribe(r.Context(), uid, r.PathValue("id"), req.Amount)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// subscribe adds amount to the user's lock in project id.  Called with s.mu held.
func (s *Server) subscribe(ctx context.Context, uid, id, raw string) (map[string]any, error) {
	amount, err := parseAmount(raw)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := s.loadProject(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if p.Status == "cancelled" {
		return nil, conflict("PROJECT_CANCELLED", "项目已取消")
	}
	if now.Before(p.StartAt) {
		return nil, conflict("WINDOW_NOT_OPEN", "申购窗口未开启")
	}
	if !now.Before(p.EndAt) {
		return nil, conflict("WINDOW_CLOSED", "申购窗口已结束")
	}
	if amount.LessThan(p.MinSub) {
		return nil, bad("AMOUNT_TOO_SMALL", "最低申购 "+p.MinSub.String()+" "+p.BaseAsset)
	}
	// Ensure a row exists, then lock and re-check caps.
	if _, err := tx.ExecContext(ctx, `INSERT INTO launchpad_subscriptions (uid, project_id, amount, locked_at, status)
		VALUES ($1,$2,0,$3,'holding') ON CONFLICT (uid, project_id) DO NOTHING`, uid, p.ID, now); err != nil {
		return nil, err
	}
	var curr decimal.Decimal
	var status string
	var subID int64
	if err := tx.QueryRowContext(ctx, `SELECT id, amount, status FROM launchpad_subscriptions
		WHERE uid=$1 AND project_id=$2 FOR UPDATE`, uid, p.ID).Scan(&subID, &curr, &status); err != nil {
		return nil, err
	}
	if status == "cancelled" {
		// Reopen on fresh subscription: reset the locked_at too.
		if _, err := tx.ExecContext(ctx, `UPDATE launchpad_subscriptions SET status='holding', locked_at=$2 WHERE id=$1`, subID, now); err != nil {
			return nil, err
		}
		curr = decimal.Zero
	} else if status != "holding" {
		return nil, conflict("SUB_LOCKED", "该申购已进入结算，不可追加")
	}
	after := curr.Add(amount)
	if p.MaxSub != nil && after.GreaterThan(*p.MaxSub) {
		return nil, bad("AMOUNT_TOO_LARGE", "超过个人申购上限 "+p.MaxSub.String()+" "+p.BaseAsset)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE launchpad_subscriptions SET amount=$2 WHERE id=$1`, subID, after); err != nil {
		return nil, err
	}
	ref := fmt.Sprintf("launchpad:sub:%d:%d", subID, time.Now().UnixNano())
	if err := s.event(ctx, tx, uid, p.ID, "subscribe", p.BaseAsset, amount, map[string]any{"ref": ref}); err != nil {
		return nil, err
	}
	if err := s.systemTransfer(ctx, uid, p.BaseAsset, "debit", amount, ref); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = s.systemTransfer(context.Background(), uid, p.BaseAsset, "credit", amount, ref+":revert")
		return nil, err
	}
	return map[string]any{
		"projectId":  p.ID,
		"baseAsset":  p.BaseAsset,
		"locked":     after,
		"added":      amount,
		"windowEnds": p.EndAt.UnixMilli(),
	}, nil
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Amount string `json:"amount"`
		All    bool   `json:"all"`
	}
	_ = common.DecodeJSON(r, &req)
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		p, err := s.loadProject(ctx, tx, r.PathValue("id"), true)
		if err != nil {
			return err
		}
		if !s.now().Before(p.EndAt) {
			return conflict("WINDOW_CLOSED", "窗口已结束，不可取消")
		}
		if p.Status == "cancelled" {
			return conflict("PROJECT_CANCELLED", "项目已取消")
		}
		var subID int64
		var amount decimal.Decimal
		var status string
		err = tx.QueryRowContext(ctx, `SELECT id, amount, status FROM launchpad_subscriptions
			WHERE uid=$1 AND project_id=$2 FOR UPDATE`, uid, p.ID).Scan(&subID, &amount, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("SUB_NOT_FOUND", "尚未申购该项目")
		}
		if err != nil {
			return err
		}
		if status != "holding" || amount.Sign() <= 0 {
			return conflict("SUB_NOT_ACTIVE", "没有可取消的申购")
		}
		var give decimal.Decimal
		if req.All || req.Amount == "" {
			give = amount
		} else {
			give, err = parseAmount(req.Amount)
			if err != nil {
				return err
			}
			if give.GreaterThan(amount) {
				return bad("AMOUNT_TOO_LARGE", "取消数量超过锁仓")
			}
		}
		remaining := amount.Sub(give)
		if remaining.Sign() > 0 && remaining.LessThan(p.MinSub) {
			// Collapse remainder into the cancel so we never leave a sub-minimum lock.
			give = amount
			remaining = decimal.Zero
		}
		newStatus := "holding"
		if remaining.Sign() == 0 {
			newStatus = "cancelled"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE launchpad_subscriptions SET amount=$2, status=$3 WHERE id=$1`, subID, remaining, newStatus); err != nil {
			return err
		}
		ref := fmt.Sprintf("launchpad:cancel:%d:%d", subID, time.Now().UnixNano())
		if err := s.event(ctx, tx, uid, p.ID, "cancel", p.BaseAsset, give, map[string]any{"ref": ref, "remaining": remaining}); err != nil {
			return err
		}
		if err := s.systemTransfer(ctx, uid, p.BaseAsset, "credit", give, ref); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			_ = s.systemTransfer(context.Background(), uid, p.BaseAsset, "debit", give, ref+":revert")
			return err
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{
			"projectId": p.ID,
			"refunded":  give,
			"remaining": remaining,
			"status":    newStatus,
		})
		return nil
	}()
	if err != nil {
		writeErr(w, err)
	}
}

// handleUserSubscription returns one subscription detail (user-scoped) plus
// its project aggregates, so the UI can render a results screen without a
// second call.
func (s *Server) handleUserSubscription(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	pid := r.PathValue("projectId")
	p, err := s.loadProject(ctx, s.db, pid, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	var sid int64
	var amount decimal.Decimal
	var avg, result, used, refund decimal.NullDecimal
	var status string
	var locked time.Time
	var settled, distributed sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT id, amount, avg_base, result_amount, used_base, refund_amount, status,
		locked_at, settled_at, distributed_at FROM launchpad_subscriptions WHERE uid=$1 AND project_id=$2`, uid, pid).
		Scan(&sid, &amount, &avg, &result, &used, &refund, &status, &locked, &settled, &distributed)
	out := map[string]any{"project": s.publicView(p)}
	if errors.Is(err, sql.ErrNoRows) {
		out["mine"] = nil
		common.WriteJSON(w, http.StatusOK, out)
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	mine := map[string]any{
		"id":       sid,
		"amount":   amount,
		"status":   status,
		"lockedAt": locked.UnixMilli(),
	}
	if avg.Valid {
		mine["avgBase"] = avg.Decimal
	}
	if result.Valid {
		mine["resultAmount"] = result.Decimal
	}
	if used.Valid {
		mine["usedBase"] = used.Decimal
	}
	if refund.Valid {
		mine["refundAmount"] = refund.Decimal
	}
	if settled.Valid {
		mine["settledAt"] = settled.Time.UnixMilli()
	}
	if distributed.Valid {
		mine["distributedAt"] = distributed.Time.UnixMilli()
	}
	out["mine"] = mine
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleUserSubscriptions(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT s.id, s.project_id, s.amount, s.avg_base, s.result_amount,
		s.used_base, s.refund_amount, s.status, s.locked_at, s.settled_at, s.distributed_at,
		p.symbol, p.base_asset, p.status, p.start_at, p.end_at, p.settle_at, p.distribute_at, p.price
		FROM launchpad_subscriptions s JOIN launchpad_projects p ON p.id = s.project_id
		WHERE s.uid=$1 ORDER BY s.id DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var sid int64
		var pid, status, symbol, baseAsset, pStatus string
		var amount, price decimal.Decimal
		var avg, result, used, refund decimal.NullDecimal
		var locked time.Time
		var settled, distributed sql.NullTime
		var startAt, endAt, settleAt, distAt time.Time
		if err := rows.Scan(&sid, &pid, &amount, &avg, &result, &used, &refund, &status, &locked,
			&settled, &distributed, &symbol, &baseAsset, &pStatus, &startAt, &endAt, &settleAt, &distAt, &price); err != nil {
			writeErr(w, err)
			return
		}
		m := map[string]any{
			"id":            sid,
			"projectId":     pid,
			"symbol":        symbol,
			"baseAsset":     baseAsset,
			"amount":        amount,
			"status":        status,
			"projectStatus": pStatus,
			"lockedAt":      locked.UnixMilli(),
			"startAt":       startAt.UnixMilli(),
			"endAt":         endAt.UnixMilli(),
			"settleAt":      settleAt.UnixMilli(),
			"distributeAt":  distAt.UnixMilli(),
			"price":         price,
		}
		if avg.Valid {
			m["avgBase"] = avg.Decimal
		}
		if result.Valid {
			m["resultAmount"] = result.Decimal
		}
		if used.Valid {
			m["usedBase"] = used.Decimal
		}
		if refund.Valid {
			m["refundAmount"] = refund.Decimal
		}
		if settled.Valid {
			m["settledAt"] = settled.Time.UnixMilli()
		}
		if distributed.Valid {
			m["distributedAt"] = distributed.Time.UnixMilli()
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
