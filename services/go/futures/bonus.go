package main

// 体验金与抵扣金不进钱包余额，本金不能划出。只代付成本：手续费先扣抵扣金再扣体验金，
// 全仓资金费只扣体验金。手续费接收方仍拿全额，差额由 system:bonus 出，守恒审计才成立。
// 费用被抵掉期间产生的盈利是普通余额，可以划出。

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

const bonusUser = "system:bonus"

// Bonus 是体验金或抵扣金的剩余额度，Remaining 才是还能抵的部分，不是钱包余额。
type Bonus struct {
	ID        int64           `json:"id"`
	Ref       string          `json:"ref"`
	Kind      string          `json:"kind"`
	Source    string          `json:"source"`
	Amount    decimal.Decimal `json:"amount"`
	Remaining decimal.Decimal `json:"remaining"`
	Status    string          `json:"status"`
	ExpiresAt int64           `json:"expiresAt"`
	CreatedAt int64           `json:"createdAt"`
}

// useBonus 从 uid 未过期的指定 kind 里扣，最多扣 amt。返回已抵金额：已从 system:bonus 划出并记回 uid（账单类型 billKind）。
// system: 与 copy: 账户不抵。抵扣金先于体验金。
func useBonus(ctx context.Context, tx *sql.Tx, uid, symbol, billKind string, amt decimal.Decimal, kinds ...string) (decimal.Decimal, error) {
	if amt.Sign() <= 0 || strings.HasPrefix(uid, "system:") || strings.HasPrefix(uid, "copy:") {
		return dZero, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, remaining FROM fut_bonus WHERE user_id=$1 AND status='active' AND expires_at > now()
		AND kind = ANY(string_to_array($2, ',')) ORDER BY CASE kind WHEN 'deduction' THEN 0 ELSE 1 END, expires_at, id FOR UPDATE`, uid, strings.Join(kinds, ","))
	if err != nil {
		return dZero, err
	}
	type rb struct {
		id  int64
		rem decimal.Decimal
	}
	var bs []rb
	for rows.Next() {
		var b rb
		if err := rows.Scan(&b.id, &b.rem); err != nil {
			rows.Close()
			return dZero, err
		}
		bs = append(bs, b)
	}
	rows.Close()
	covered := dZero
	for _, b := range bs {
		need := amt.Sub(covered)
		if need.Sign() <= 0 {
			break
		}
		take := dmin(need, b.rem)
		left := b.rem.Sub(take)
		st := "active"
		if left.Sign() <= 0 {
			st = "used"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE fut_bonus SET remaining=$2, status=$3 WHERE id=$1`, b.id, left, st); err != nil {
			return dZero, err
		}
		covered = covered.Add(take)
	}
	if covered.Sign() <= 0 {
		return dZero, nil
	}
	if err := addBalance(ctx, tx, bonusUser, symbol, billKind, covered.Neg(), uid); err != nil {
		return dZero, err
	}
	if err := addBalance(ctx, tx, uid, symbol, billKind, covered, ""); err != nil {
		return dZero, err
	}
	return covered, nil
}

func (s *Server) bonusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /futures/bonus", s.user(func(w http.ResponseWriter, r *http.Request, uid string) {
		s.writeBonuses(w, r, uid)
	}))
	mux.HandleFunc("GET /internal/bonus", s.internalOnly(func(w http.ResponseWriter, r *http.Request) {
		s.writeBonuses(w, r, r.URL.Query().Get("userId"))
	}))
	mux.HandleFunc("POST /internal/bonus/grant", s.internalOnly(s.hBonusGrant))
}

func (s *Server) writeBonuses(w http.ResponseWriter, r *http.Request, uid string) {
	_, _ = s.db.ExecContext(r.Context(), `UPDATE fut_bonus SET status='expired' WHERE user_id=$1 AND status='active' AND expires_at <= now()`, uid)
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, ref, kind, source, amount, remaining, status, expires_at, created_at FROM fut_bonus
		WHERE user_id=$1 ORDER BY id DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []Bonus{}
	avail := map[string]decimal.Decimal{"trial": dZero, "deduction": dZero}
	for rows.Next() {
		var b Bonus
		var exp, at time.Time
		if err := rows.Scan(&b.ID, &b.Ref, &b.Kind, &b.Source, &b.Amount, &b.Remaining, &b.Status, &exp, &at); err != nil {
			writeErr(w, err)
			return
		}
		b.ExpiresAt, b.CreatedAt = exp.UnixMilli(), at.UnixMilli()
		if b.Status == "active" {
			avail[b.Kind] = avail[b.Kind].Add(b.Remaining)
		}
		items = append(items, b)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "available": avail})
}

// POST /internal/bonus/grant {userId, ref, kind, amount, validDays, source, requireOwnFunds}
// 按 ref 幂等。requireOwnFunds 的体验金要求合约钱包自有余额至少等于赠送额。
func (s *Server) hBonusGrant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID          string          `json:"userId"`
		Ref             string          `json:"ref"`
		Kind            string          `json:"kind"`
		Amount          decimal.Decimal `json:"amount"`
		ValidDays       int             `json:"validDays"`
		Source          string          `json:"source"`
		RequireOwnFunds bool            `json:"requireOwnFunds"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.UserID == "" || req.Ref == "" || req.Amount.Sign() <= 0 || req.ValidDays <= 0 || (req.Kind != "trial" && req.Kind != "deduction") {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	var out Bonus
	err := s.withTx(r.Context(), func(tx *sql.Tx) error {
		var exp, at time.Time
		err := tx.QueryRowContext(r.Context(), `SELECT id, ref, kind, source, amount, remaining, status, expires_at, created_at FROM fut_bonus WHERE ref=$1`, req.Ref).
			Scan(&out.ID, &out.Ref, &out.Kind, &out.Source, &out.Amount, &out.Remaining, &out.Status, &exp, &at)
		if err == nil {
			out.ExpiresAt, out.CreatedAt = exp.UnixMilli(), at.UnixMilli()
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		if req.RequireOwnFunds {
			bal, _, err := lockAccount(r.Context(), tx, req.UserID)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if bal.LessThan(req.Amount) {
				return &apiError{http.StatusConflict, "INSUFFICIENT_OWN_FUNDS", "合约账户自有资金不足，需不少于体验金金额"}
			}
		}
		return tx.QueryRowContext(r.Context(), `INSERT INTO fut_bonus (user_id, ref, kind, source, amount, remaining, status, expires_at)
			VALUES ($1,$2,$3,$4,$5,$5,'active', now() + make_interval(days => $6))
			RETURNING id, ref, kind, source, amount, remaining, status, expires_at, created_at`, req.UserID, req.Ref, req.Kind, req.Source, req.Amount, req.ValidDays).
			Scan(&out.ID, &out.Ref, &out.Kind, &out.Source, &out.Amount, &out.Remaining, &out.Status, &exp, &at)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}
