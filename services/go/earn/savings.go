package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Savings products. Money moves between the user's spot balance and the
// system "earn" account (principal) and "earn_interest" (yield).
//
//   simple   flexible, subscribe/redeem any time; interest from the next UTC
//            day, paid daily; bonus_apr on the first bonus_quota, apr above it;
//            optional auto-earn sweeps idle spot balance at each daily run.
//   fixed    理财宝: locked for term_days, no early redemption; principal +
//            interest (amount × apr × days / 365) returned at maturity.
//   onchain  staking: daily interest like simple; redemption waits
//            unlock_days (no interest while unlocking) unless it is 0.

const day = 24 * time.Hour

var days365 = decimal.NewFromInt(365)

func floor8(d decimal.Decimal) decimal.Decimal { return d.RoundFloor(dp) }

type product struct {
	ID         string           `json:"id"`
	Kind       string           `json:"kind"`
	Asset      string           `json:"asset"`
	Title      string           `json:"title"`
	APR        decimal.Decimal  `json:"apr"`
	BonusAPR   *decimal.Decimal `json:"bonusApr,omitempty"`
	BonusQuota *decimal.Decimal `json:"bonusQuota,omitempty"`
	TermDays   int              `json:"termDays"`
	UnlockDays int              `json:"unlockDays"`
	MinAmount  decimal.Decimal  `json:"minAmount"`
	PerUserMax *decimal.Decimal `json:"perUserMax,omitempty"`
	TotalQuota *decimal.Decimal `json:"totalQuota,omitempty"`
	Sold       decimal.Decimal  `json:"sold"`
	Newcomer   bool             `json:"newcomer"`
	Hot        bool             `json:"hot"`
	Status     string           `json:"status"`
	Sort       int              `json:"sort"`
	APRMax     decimal.Decimal  `json:"aprMax"`
	Remaining  *decimal.Decimal `json:"remaining,omitempty"`
	RedeemRule string           `json:"redeemRule"`
}

const productCols = `id, kind, asset, title, apr, bonus_apr, bonus_quota, term_days, unlock_days, min_amount, per_user_max, total_quota, sold, newcomer, hot, status, sort`

type scanner interface{ Scan(...any) error }

func scanProduct(r scanner) (*product, error) {
	p := &product{}
	var ba, bq, pm, tq decimal.NullDecimal
	if err := r.Scan(&p.ID, &p.Kind, &p.Asset, &p.Title, &p.APR, &ba, &bq, &p.TermDays, &p.UnlockDays, &p.MinAmount, &pm, &tq,
		&p.Sold, &p.Newcomer, &p.Hot, &p.Status, &p.Sort); err != nil {
		return nil, err
	}
	nd := func(n decimal.NullDecimal) *decimal.Decimal {
		if !n.Valid {
			return nil
		}
		v := n.Decimal
		return &v
	}
	p.BonusAPR, p.BonusQuota, p.PerUserMax, p.TotalQuota = nd(ba), nd(bq), nd(pm), nd(tq)
	p.APRMax = p.APR
	if p.BonusAPR != nil && p.BonusAPR.GreaterThan(p.APR) {
		p.APRMax = *p.BonusAPR
	}
	if p.TotalQuota != nil {
		r := p.TotalQuota.Sub(p.Sold)
		p.Remaining = &r
	}
	switch {
	case p.Kind == "fixed":
		p.RedeemRule = "到期自动返还，不可提前赎回"
	case p.Kind == "onchain" && p.UnlockDays > 0:
		p.RedeemRule = strconv.Itoa(p.UnlockDays) + " 天解锁"
	default:
		p.RedeemRule = "随时赎回"
	}
	return p, nil
}

func (s *Server) loadProduct(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, lock bool) (*product, error) {
	sfx := ""
	if lock {
		sfx = " FOR UPDATE"
	}
	p, err := scanProduct(q.QueryRowContext(ctx, `SELECT `+productCols+` FROM e_products WHERE id=$1`+sfx, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("PRODUCT_NOT_FOUND", "理财产品不存在")
	}
	return p, err
}

// dailyInterest is one day of yield on principal with the bonus tier.
func (p *product) dailyInterest(principal decimal.Decimal) decimal.Decimal {
	if principal.Sign() <= 0 {
		return decimal.Zero
	}
	yearly := principal.Mul(p.APR)
	if p.BonusAPR != nil && p.BonusQuota != nil {
		b := decimal.Min(principal, *p.BonusQuota)
		yearly = b.Mul(*p.BonusAPR).Add(principal.Sub(b).Mul(p.APR))
	}
	return floor8(yearly.Div(days365))
}

func (s *Server) dayIndex(t time.Time) int { return int(t.Unix() / 86400) }

func (s *Server) savingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /earn/products", s.handleProducts)
	mux.HandleFunc("GET /earn/products/{id}", s.handleProduct)
	mux.HandleFunc("POST /earn/products/{id}/subscribe", s.user(s.handleSubscribe))
	mux.HandleFunc("POST /earn/products/{id}/redeem", s.user(s.handleRedeem))
	mux.HandleFunc("POST /earn/products/{id}/auto", s.user(s.handleAuto))
	mux.HandleFunc("GET /earn/summary", s.user(s.handleEarnSummary))
	mux.HandleFunc("GET /earn/positions", s.user(s.handlePositions))
	mux.HandleFunc("GET /earn/orders", s.user(s.handleFixedOrders))
	mux.HandleFunc("GET /earn/records", s.user(s.handleEarnRecords))
	mux.HandleFunc("POST /internal/earn/products", s.internal(s.handleUpsertProduct))
}

func (s *Server) handleProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := []string{"status='online'"}, []any{}
	if k := q.Get("kind"); k != "" {
		args = append(args, k)
		where = append(where, fmt.Sprintf("kind=$%d", len(args)))
	}
	if a := strings.ToUpper(q.Get("asset")); a != "" {
		args = append(args, a)
		where = append(where, fmt.Sprintf("asset=$%d", len(args)))
	}
	if q.Get("hot") == "true" {
		where = append(where, "hot")
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+productCols+` FROM e_products WHERE `+strings.Join(where, " AND ")+` ORDER BY sort, id`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleProduct(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadProduct(r.Context(), s.db, r.PathValue("id"), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, p)
}

func (s *Server) earnEvent(ctx context.Context, tx *sql.Tx, uid string, p *product, kind string, amount decimal.Decimal, detail map[string]any) error {
	d := []byte("{}")
	if detail != nil {
		d, _ = json.Marshal(detail)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO e_earn_events (uid, product_id, kind, asset, amount, detail, at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uid, p.ID, kind, p.Asset, amount, d, s.now())
	return err
}

type holding struct {
	ID            int64
	UID           string
	Principal     decimal.Decimal
	Pending       decimal.Decimal
	PendingDay    int
	LastDay       int
	InterestTotal decimal.Decimal
	Auto          bool
}

// loadHolding locks (creating if needed) the user's holding in p.
func (s *Server) loadHolding(ctx context.Context, tx *sql.Tx, uid string, p *product) (*holding, error) {
	today := s.dayIndex(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO e_holdings (uid, product_id, last_day) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, uid, p.ID, today-1); err != nil {
		return nil, err
	}
	h := &holding{UID: uid}
	err := tx.QueryRowContext(ctx, `SELECT id, principal, pending, pending_day, last_day, interest_total, auto FROM e_holdings
		WHERE uid=$1 AND product_id=$2 FOR UPDATE`, uid, p.ID).Scan(&h.ID, &h.Principal, &h.Pending, &h.PendingDay, &h.LastDay, &h.InterestTotal, &h.Auto)
	return h, err
}

// settle pays every complete UTC day since last_day and moves earlier
// subscriptions into the earning principal. It must run inside tx before
// any change to the holding.
func (s *Server) settle(ctx context.Context, tx *sql.Tx, p *product, h *holding) error {
	today := s.dayIndex(s.now())
	total := decimal.Zero
	from := h.LastDay + 1
	for k := from; k < today; k++ {
		if h.Pending.Sign() > 0 && h.PendingDay < k {
			h.Principal, h.Pending = h.Principal.Add(h.Pending), decimal.Zero
		}
		total = total.Add(p.dailyInterest(h.Principal))
	}
	if h.Pending.Sign() > 0 && h.PendingDay < today {
		h.Principal, h.Pending = h.Principal.Add(h.Pending), decimal.Zero
	}
	if today-1 > h.LastDay {
		h.LastDay = today - 1
	}
	if total.Sign() > 0 {
		if err := s.systemTransfer(ctx, h.UID, p.Asset, "earn_interest", "credit", total, fmt.Sprintf("earn:h:%d:%d-%d", h.ID, from, h.LastDay)); err != nil {
			return err
		}
		h.InterestTotal = h.InterestTotal.Add(total)
		if err := s.earnEvent(ctx, tx, h.UID, p, "interest", total, map[string]any{"fromDay": from, "toDay": h.LastDay}); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE e_holdings SET principal=$2, pending=$3, pending_day=$4, last_day=$5, interest_total=$6 WHERE id=$1`,
		h.ID, h.Principal, h.Pending, h.PendingDay, h.LastDay, h.InterestTotal)
	return err
}

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
	out, err := s.subscribe(r.Context(), uid, r.PathValue("id"), req.Amount, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// subscribe moves amount from spot into product id. Caller holds s.mu.
func (s *Server) subscribe(ctx context.Context, uid, id, raw string, auto bool) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := s.loadProduct(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if p.Status != "online" {
		return nil, conflict("PRODUCT_OFFLINE", "产品已下架")
	}
	amount, err := parseAmount(raw)
	if err != nil {
		return nil, err
	}
	if amount.LessThan(p.MinAmount) {
		return nil, bad("AMOUNT_TOO_SMALL", "最低申购 "+p.MinAmount.String()+" "+p.Asset)
	}
	if p.TotalQuota != nil && p.Sold.Add(amount).GreaterThan(*p.TotalQuota) {
		return nil, conflict("QUOTA_EXCEEDED", "产品剩余额度不足")
	}
	if p.Newcomer {
		var other int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM e_fixed_orders o JOIN e_products p ON p.id=o.product_id
			WHERE o.uid=$1 AND o.status<>'failed' AND NOT p.newcomer`, uid).Scan(&other); err != nil {
			return nil, err
		}
		if other > 0 {
			return nil, conflict("NEWCOMER_ONLY", "该产品仅限新用户申购")
		}
	}
	now := s.now()
	ref := fmt.Sprintf("earn:sub:%s:%s:%d", id, uid, time.Now().UnixNano())
	out := map[string]any{"productId": p.ID, "asset": p.Asset, "amount": amount}
	if p.Kind == "fixed" {
		if p.PerUserMax != nil {
			var held decimal.Decimal
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM e_fixed_orders WHERE uid=$1 AND product_id=$2 AND status<>'failed'`,
				uid, p.ID).Scan(&held); err != nil {
				return nil, err
			}
			if held.Add(amount).GreaterThan(*p.PerUserMax) {
				return nil, bad("USER_LIMIT_EXCEEDED", "超过个人申购上限 "+p.PerUserMax.String()+" "+p.Asset)
			}
		}
		interest := floor8(amount.Mul(p.APR).Mul(decimal.NewFromInt(int64(p.TermDays))).Div(days365))
		var oid int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO e_fixed_orders (uid, product_id, asset, amount, apr, term_days, interest, status, start_at, maturity_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'holding',$8,$9) RETURNING id`, uid, p.ID, p.Asset, amount, p.APR, p.TermDays, interest, now,
			now.Add(time.Duration(p.TermDays)*day)).Scan(&oid); err != nil {
			return nil, err
		}
		out["orderId"], out["expectedInterest"], out["maturityAt"] = oid, interest, now.Add(time.Duration(p.TermDays)*day).UnixMilli()
	} else {
		h, err := s.loadHolding(ctx, tx, uid, p)
		if err != nil {
			return nil, err
		}
		if err := s.settle(ctx, tx, p, h); err != nil {
			return nil, err
		}
		if p.PerUserMax != nil && h.Principal.Add(h.Pending).Add(amount).GreaterThan(*p.PerUserMax) {
			return nil, bad("USER_LIMIT_EXCEEDED", "超过个人申购上限 "+p.PerUserMax.String()+" "+p.Asset)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE e_holdings SET pending=pending+$2, pending_day=$3 WHERE id=$1`, h.ID, amount, s.dayIndex(now)); err != nil {
			return nil, err
		}
		out["interestStartsAt"] = time.Unix(int64(s.dayIndex(now)+1)*86400, 0).UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE e_products SET sold=sold+$2 WHERE id=$1`, p.ID, amount); err != nil {
		return nil, err
	}
	if err := s.earnEvent(ctx, tx, uid, p, "subscribe", amount, map[string]any{"auto": auto}); err != nil {
		return nil, err
	}
	if err := s.systemTransfer(ctx, uid, p.Asset, "earn", "debit", amount, ref); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = s.systemTransfer(context.Background(), uid, p.Asset, "earn", "credit", amount, ref+":revert")
		return nil, err
	}
	return out, nil
}

func (s *Server) handleRedeem(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Amount string `json:"amount"`
		All    bool   `json:"all"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		p, err := s.loadProduct(ctx, tx, r.PathValue("id"), true)
		if err != nil {
			return err
		}
		if p.Kind == "fixed" {
			return conflict("REDEEM_NOT_ALLOWED", "定期产品到期前不可赎回")
		}
		h, err := s.loadHolding(ctx, tx, uid, p)
		if err != nil {
			return err
		}
		if err := s.settle(ctx, tx, p, h); err != nil {
			return err
		}
		held := h.Principal.Add(h.Pending)
		amount := held
		if !req.All {
			if amount, err = parseAmount(req.Amount); err != nil {
				return err
			}
		}
		if amount.Sign() <= 0 || amount.GreaterThan(held) {
			return bad("INSUFFICIENT_POSITION", "可赎回数量不足")
		}
		fromPending := decimal.Min(amount, h.Pending)
		h.Pending, h.Principal = h.Pending.Sub(fromPending), h.Principal.Sub(amount.Sub(fromPending))
		if _, err := tx.ExecContext(ctx, `UPDATE e_holdings SET principal=$2, pending=$3 WHERE id=$1`, h.ID, h.Principal, h.Pending); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE e_products SET sold=GREATEST(sold-$2,0) WHERE id=$1`, p.ID, amount); err != nil {
			return err
		}
		now := s.now()
		detail := map[string]any{}
		if p.Kind == "onchain" && p.UnlockDays > 0 {
			rel := now.Add(time.Duration(p.UnlockDays) * day)
			var uidl int64
			if err := tx.QueryRowContext(ctx, `INSERT INTO e_unlocks (uid, product_id, asset, amount, release_at, status, created_at)
				VALUES ($1,$2,$3,$4,$5,'unlocking',$6) RETURNING id`, uid, p.ID, p.Asset, amount, rel, now).Scan(&uidl); err != nil {
				return err
			}
			detail["unlockId"], detail["releaseAt"] = uidl, rel.UnixMilli()
			if err := s.earnEvent(ctx, tx, uid, p, "redeem", amount, detail); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			common.WriteJSON(w, http.StatusOK, map[string]any{"productId": p.ID, "amount": amount, "status": "unlocking", "releaseAt": rel.UnixMilli()})
			return nil
		}
		if err := s.earnEvent(ctx, tx, uid, p, "redeem", amount, detail); err != nil {
			return err
		}
		ref := fmt.Sprintf("earn:red:%s:%s:%d", p.ID, uid, time.Now().UnixNano())
		if err := s.systemTransfer(ctx, uid, p.Asset, "earn", "credit", amount, ref); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			_ = s.systemTransfer(context.Background(), uid, p.Asset, "earn", "debit", amount, ref+":revert")
			return err
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"productId": p.ID, "amount": amount, "status": "redeemed"})
		return nil
	}()
	if err != nil {
		writeErr(w, err)
	}
}

func (s *Server) handleAuto(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		p, err := s.loadProduct(ctx, tx, r.PathValue("id"), false)
		if err != nil {
			return err
		}
		if p.Kind != "simple" {
			return bad("AUTO_NOT_SUPPORTED", "仅活期产品支持自动赚币")
		}
		h, err := s.loadHolding(ctx, tx, uid, p)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE e_holdings SET auto=$2 WHERE id=$1`, h.ID, req.Enabled); err != nil {
			return err
		}
		return tx.Commit()
	}()
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"productId": r.PathValue("id"), "auto": req.Enabled})
}

func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	kind := r.URL.Query().Get("kind")
	rows, err := s.db.QueryContext(ctx, `SELECT `+prefixed("p", productCols)+`, h.principal, h.pending, h.interest_total, h.auto, h.last_day
		FROM e_holdings h JOIN e_products p ON p.id=h.product_id
		WHERE h.uid=$1 AND ($2='' OR p.kind=$2) AND (h.principal+h.pending>0 OR h.interest_total>0 OR h.auto) ORDER BY p.sort`, uid, kind)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	today := s.dayIndex(s.now())
	for rows.Next() {
		var p product
		var ba, bq, pm, tq decimal.NullDecimal
		var prin, pend, tot decimal.Decimal
		var auto bool
		var last int
		if err := rows.Scan(&p.ID, &p.Kind, &p.Asset, &p.Title, &p.APR, &ba, &bq, &p.TermDays, &p.UnlockDays, &p.MinAmount, &pm, &tq,
			&p.Sold, &p.Newcomer, &p.Hot, &p.Status, &p.Sort, &prin, &pend, &tot, &auto, &last); err != nil {
			writeErr(w, err)
			return
		}
		if ba.Valid {
			p.BonusAPR = &ba.Decimal
		}
		if bq.Valid {
			p.BonusQuota = &bq.Decimal
		}
		// Unsettled complete days (the ticker pays them shortly).
		earning := prin
		if last < today-1 && pend.Sign() > 0 {
			earning = prin.Add(pend)
		}
		items = append(items, map[string]any{"productId": p.ID, "kind": p.Kind, "asset": p.Asset, "title": p.Title,
			"amount": prin.Add(pend), "earning": earning, "pending": pend, "totalInterest": tot, "auto": auto,
			"estDailyInterest": p.dailyInterest(prin.Add(pend))})
	}
	ur, err := s.db.QueryContext(ctx, `SELECT u.id, u.product_id, u.asset, u.amount, u.release_at, p.kind FROM e_unlocks u JOIN e_products p ON p.id=u.product_id
		WHERE u.uid=$1 AND u.status='unlocking' ORDER BY u.release_at`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer ur.Close()
	unlocks := []map[string]any{}
	for ur.Next() {
		var id int64
		var pid, asset, k string
		var amt decimal.Decimal
		var rel time.Time
		if err := ur.Scan(&id, &pid, &asset, &amt, &rel, &k); err != nil {
			writeErr(w, err)
			return
		}
		if kind == "" || kind == k {
			unlocks = append(unlocks, map[string]any{"id": id, "productId": pid, "asset": asset, "amount": amt, "releaseAt": rel.UnixMilli()})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "unlocking": unlocks})
}

func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

func (s *Server) handleFixedOrders(w http.ResponseWriter, r *http.Request, uid string) {
	st := r.URL.Query().Get("status")
	if st != "" && st != "holding" && st != "matured" {
		writeErr(w, bad("VALIDATION_ERROR", "status 不合法"))
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT o.id, o.product_id, p.title, o.asset, o.amount, o.apr, o.term_days, o.interest, o.status,
		o.start_at, o.maturity_at, o.settled_at FROM e_fixed_orders o JOIN e_products p ON p.id=o.product_id
		WHERE o.uid=$1 AND o.status<>'failed' AND ($2='' OR o.status=$2) ORDER BY o.id DESC LIMIT 200`, uid, st)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var pid, title, asset, status string
		var amt, apr, intr decimal.Decimal
		var term int
		var start, mat time.Time
		var settled sql.NullTime
		if err := rows.Scan(&id, &pid, &title, &asset, &amt, &apr, &term, &intr, &status, &start, &mat, &settled); err != nil {
			writeErr(w, err)
			return
		}
		m := map[string]any{"id": id, "productId": pid, "title": title, "asset": asset, "amount": amt, "apr": apr, "termDays": term,
			"interest": intr, "status": status, "startAt": start.UnixMilli(), "maturityAt": mat.UnixMilli()}
		if settled.Valid {
			m["settledAt"] = settled.Time.UnixMilli()
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleEarnRecords(w http.ResponseWriter, r *http.Request, uid string) {
	t := r.URL.Query().Get("type")
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, product_id, kind, asset, amount, detail, at FROM e_earn_events
		WHERE uid=$1 AND ($2='' OR kind=$2) ORDER BY id DESC LIMIT 200`, uid, t)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var pid, kind, asset string
		var amt decimal.Decimal
		var detail []byte
		var at time.Time
		if err := rows.Scan(&id, &pid, &kind, &asset, &amt, &detail, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "productId": pid, "type": kind, "asset": asset, "amount": amt,
			"detail": json.RawMessage(detail), "time": at.UnixMilli()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleEarnSummary values holdings in USDT via the futures index (stablecoins
// at 1, assets without an index are listed but not valued).
func (s *Server) handleEarnSummary(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `
		SELECT asset, SUM(amt), SUM(intr), SUM(recent) FROM (
		  SELECT p.asset, h.principal+h.pending AS amt, h.interest_total AS intr, 0 AS recent
		    FROM e_holdings h JOIN e_products p ON p.id=h.product_id WHERE h.uid=$1
		  UNION ALL SELECT asset, CASE WHEN status='holding' THEN amount ELSE 0 END, CASE WHEN status='matured' THEN interest ELSE 0 END, 0
		    FROM e_fixed_orders WHERE uid=$1 AND status IN ('holding','matured')
		  UNION ALL SELECT asset, amount, 0, 0 FROM e_unlocks WHERE uid=$1 AND status='unlocking'
		  UNION ALL SELECT asset, 0, 0, amount FROM e_earn_events WHERE uid=$1 AND kind='interest' AND at >= $2
		) x GROUP BY asset ORDER BY asset`, uid, time.Unix(int64(s.dayIndex(s.now())-1)*86400, 0))
	if err != nil {
		writeErr(w, err)
		return
	}
	type agg struct {
		asset             string
		amt, intr, recent decimal.Decimal
	}
	var list []agg
	for rows.Next() {
		var a agg
		if err := rows.Scan(&a.asset, &a.amt, &a.intr, &a.recent); err != nil {
			rows.Close()
			writeErr(w, err)
			return
		}
		list = append(list, a)
	}
	rows.Close()
	price := func(a string) (decimal.Decimal, bool) {
		if a == "USDT" || a == "USDC" {
			return decimal.NewFromInt(1), true
		}
		p, err := s.price(ctx, a)
		return p, err == nil
	}
	var totV, totI, totR decimal.Decimal
	assets := []map[string]any{}
	for _, a := range list {
		m := map[string]any{"asset": a.asset, "amount": a.amt, "totalInterest": a.intr, "recentInterest": a.recent}
		if p, ok := price(a.asset); ok {
			v := a.amt.Mul(p).Round(2)
			m["valueUsdt"] = v
			totV, totI, totR = totV.Add(v), totI.Add(a.intr.Mul(p)), totR.Add(a.recent.Mul(p))
		}
		assets = append(assets, m)
	}
	// recentInterest: daily interest paid since the start of the previous UTC day.
	common.WriteJSON(w, http.StatusOK, map[string]any{"totalValueUsdt": totV.Round(2), "totalInterestUsdt": totI.Round(2),
		"recentInterestUsdt": totR.Round(2), "assets": assets})
}

func (s *Server) handleUpsertProduct(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ID         string           `json:"id"`
		Kind       string           `json:"kind"`
		Asset      string           `json:"asset"`
		Title      string           `json:"title"`
		APR        decimal.Decimal  `json:"apr"`
		BonusAPR   *decimal.Decimal `json:"bonusApr"`
		BonusQuota *decimal.Decimal `json:"bonusQuota"`
		TermDays   int              `json:"termDays"`
		UnlockDays int              `json:"unlockDays"`
		MinAmount  decimal.Decimal  `json:"minAmount"`
		PerUserMax *decimal.Decimal `json:"perUserMax"`
		TotalQuota *decimal.Decimal `json:"totalQuota"`
		Newcomer   bool             `json:"newcomer"`
		Hot        bool             `json:"hot"`
		Status     string           `json:"status"`
		Sort       int              `json:"sort"`
	}
	if err := common.DecodeJSON(r, &p); err != nil || p.ID == "" || p.Asset == "" || p.Title == "" || p.MinAmount.Sign() <= 0 || p.APR.Sign() < 0 ||
		(p.Kind != "simple" && p.Kind != "fixed" && p.Kind != "onchain") || (p.Kind == "fixed" && p.TermDays <= 0) {
		writeErr(w, bad("VALIDATION_ERROR", "产品参数不合法"))
		return
	}
	if p.Status == "" {
		p.Status = "online"
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO e_products (id, kind, asset, title, apr, bonus_apr, bonus_quota, term_days, unlock_days, min_amount,
		per_user_max, total_quota, newcomer, hot, status, sort) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (id) DO UPDATE SET title=$4, apr=$5, bonus_apr=$6, bonus_quota=$7, unlock_days=$9, min_amount=$10, per_user_max=$11,
		total_quota=$12, newcomer=$13, hot=$14, status=$15, sort=$16`,
		p.ID, p.Kind, strings.ToUpper(p.Asset), p.Title, p.APR, p.BonusAPR, p.BonusQuota, p.TermDays, p.UnlockDays, p.MinAmount, p.PerUserMax,
		p.TotalQuota, p.Newcomer, p.Hot, p.Status, p.Sort)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": p.ID})
}

// savingsTick settles daily interest, sweeps auto-earn balances, returns
// matured fixed orders and releases finished unlocks.
func (s *Server) savingsTick(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := s.dayIndex(s.now())
	rows, err := s.db.QueryContext(ctx, `SELECT h.uid, h.product_id, h.auto FROM e_holdings h
		WHERE h.last_day < $1 - 1 OR h.auto OR (h.pending > 0 AND h.pending_day < $1)`, today)
	if err != nil {
		return err
	}
	type hk struct {
		uid, pid string
		auto     bool
	}
	var hs []hk
	for rows.Next() {
		var k hk
		if err := rows.Scan(&k.uid, &k.pid, &k.auto); err != nil {
			rows.Close()
			return err
		}
		hs = append(hs, k)
	}
	rows.Close()
	for _, k := range hs {
		if err := s.settleOne(ctx, k.uid, k.pid); err != nil {
			log.Printf("earn: settle %s/%s: %v", k.uid, k.pid, err)
			continue
		}
		if k.auto {
			s.autoSweep(ctx, k.uid, k.pid)
		}
	}
	if err := s.matureFixed(ctx); err != nil {
		return err
	}
	return s.releaseUnlocks(ctx)
}

func (s *Server) settleOne(ctx context.Context, uid, pid string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.loadProduct(ctx, tx, pid, false)
	if err != nil {
		return err
	}
	h, err := s.loadHolding(ctx, tx, uid, p)
	if err != nil {
		return err
	}
	if err := s.settle(ctx, tx, p, h); err != nil {
		return err
	}
	return tx.Commit()
}

// autoSweep subscribes the user's free spot balance of the product asset.
func (s *Server) autoSweep(ctx context.Context, uid, pid string) {
	p, err := s.loadProduct(ctx, s.db, pid, false)
	if err != nil || p.Status != "online" {
		return
	}
	var bal struct {
		Items []struct {
			Asset string          `json:"asset"`
			Free  decimal.Decimal `json:"free"`
		} `json:"items"`
	}
	if err := s.call(ctx, "GET", s.cfg.AssetURL+"/internal/balances?userId="+uid, nil, &bal); err != nil {
		log.Printf("earn: auto balances %s: %v", uid, err)
		return
	}
	for _, b := range bal.Items {
		if b.Asset == p.Asset && b.Free.GreaterThanOrEqual(p.MinAmount) {
			if _, err := s.subscribe(ctx, uid, pid, floor8(b.Free).String(), true); err != nil {
				log.Printf("earn: auto subscribe %s/%s: %v", uid, pid, err)
			}
		}
	}
}

func (s *Server) matureFixed(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM e_fixed_orders WHERE status='holding' AND maturity_at <= $1 ORDER BY id LIMIT 500`, s.now())
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.matureOne(ctx, id); err != nil {
			log.Printf("earn: mature %d: %v", id, err)
		}
	}
	return nil
}

func (s *Server) matureOne(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var uid, pid, status string
	var amt, intr decimal.Decimal
	if err := tx.QueryRowContext(ctx, `SELECT uid, product_id, amount, interest, status FROM e_fixed_orders WHERE id=$1 FOR UPDATE`, id).
		Scan(&uid, &pid, &amt, &intr, &status); err != nil {
		return err
	}
	if status != "holding" {
		return nil
	}
	p, err := s.loadProduct(ctx, tx, pid, false)
	if err != nil {
		return err
	}
	// Deterministic refs: a retried maturity is not paid twice.
	if err := s.systemTransfer(ctx, uid, p.Asset, "earn", "credit", amt, fmt.Sprintf("earn:fix:%d:principal", id)); err != nil {
		return err
	}
	if intr.Sign() > 0 {
		if err := s.systemTransfer(ctx, uid, p.Asset, "earn_interest", "credit", intr, fmt.Sprintf("earn:fix:%d:interest", id)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE e_fixed_orders SET status='matured', settled_at=$2 WHERE id=$1`, id, s.now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE e_products SET sold=GREATEST(sold-$2,0) WHERE id=$1`, pid, amt); err != nil {
		return err
	}
	if err := s.earnEvent(ctx, tx, uid, p, "maturity", amt, map[string]any{"orderId": id, "interest": intr}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) releaseUnlocks(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM e_unlocks WHERE status='unlocking' AND release_at <= $1 ORDER BY id LIMIT 500`, s.now())
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		err := func() error {
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			var uid, pid, status string
			var amt decimal.Decimal
			if err := tx.QueryRowContext(ctx, `SELECT uid, product_id, amount, status FROM e_unlocks WHERE id=$1 FOR UPDATE`, id).
				Scan(&uid, &pid, &amt, &status); err != nil || status != "unlocking" {
				return err
			}
			p, err := s.loadProduct(ctx, tx, pid, false)
			if err != nil {
				return err
			}
			if err := s.systemTransfer(ctx, uid, p.Asset, "earn", "credit", amt, fmt.Sprintf("earn:unlock:%d", id)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE e_unlocks SET status='released' WHERE id=$1`, id); err != nil {
				return err
			}
			if err := s.earnEvent(ctx, tx, uid, p, "unlock", amt, map[string]any{"unlockId": id}); err != nil {
				return err
			}
			return tx.Commit()
		}()
		if err != nil {
			log.Printf("earn: unlock %d: %v", id, err)
		}
	}
	return nil
}
