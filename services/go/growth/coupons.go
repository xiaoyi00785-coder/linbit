package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Coupon center (卡券中心). Every reward program issues vouchers here; the
// user activates a voucher, which grants the bonus in the futures service.
type Coupon struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"` // trial (体验金) | deduction (抵扣金)
	Amount      string     `json:"amount"`
	Asset       string     `json:"asset"`
	Title       string     `json:"title"`
	Status      string     `json:"status"` // issued | active | expired | revoked
	ActivateBy  int64      `json:"activateBy"`
	ValidDays   int        `json:"validDays"`
	ActivatedAt *int64     `json:"activatedAt,omitempty"`
	CreatedAt   int64      `json:"createdAt"`
	Bonus       *bonusView `json:"bonus,omitempty"`
}

type bonusView struct {
	Remaining string `json:"remaining"`
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (s *Server) couponRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /coupons", s.user(s.handleCoupons))
	mux.HandleFunc("GET /coupons/summary", s.user(s.handleCouponSummary))
	mux.HandleFunc("POST /coupons/{id}/activate", s.user(s.handleCouponActivate))
	mux.HandleFunc("GET /internal/coupons", s.internal(func(w http.ResponseWriter, r *http.Request) {
		uid := r.URL.Query().Get("userId")
		if uid == "" {
			common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "缺少 userId")
			return
		}
		s.handleCoupons(w, r, uid)
	}))
}

// issueCoupon creates a voucher; idempotent on source. Returns the id.
func (s *Server) issueCoupon(ctx context.Context, q rowQueryer, uid, kind string, amount decimal.Decimal, title, source string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `INSERT INTO g_coupons (uid, kind, amount, title, source, status, activate_by, valid_days)
		VALUES ($1,$2,$3,$4,$5,'issued',$6,$7) ON CONFLICT (source) DO NOTHING RETURNING id`,
		uid, kind, amount, title, source, s.now().Add(time.Duration(s.cfg.CouponActivateDays)*24*time.Hour), s.cfg.CouponValidDays).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = q.QueryRowContext(ctx, `SELECT id FROM g_coupons WHERE source=$1`, source).Scan(&id)
	}
	return id, err
}

func (s *Server) expireCoupons(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE g_coupons SET status='expired' WHERE status='issued' AND activate_by < $1`, s.now())
}

func (s *Server) handleCoupons(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	s.expireCoupons(ctx)
	q := `SELECT id, kind, amount, title, status, activate_by, valid_days, activated_at, created_at FROM g_coupons WHERE uid=$1`
	args := []any{uid}
	if st := r.URL.Query().Get("status"); st != "" {
		q += ` AND status=$2`
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT 200`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*Coupon{}
	hasActive := false
	for rows.Next() {
		c := &Coupon{Asset: s.cfg.PayoutAsset}
		var amt decimal.Decimal
		var by, created time.Time
		var act sql.NullTime
		if err := rows.Scan(&c.ID, &c.Kind, &amt, &c.Title, &c.Status, &by, &c.ValidDays, &act, &created); err != nil {
			writeErr(w, err)
			return
		}
		c.Amount, c.ActivateBy, c.CreatedAt = amt.String(), by.UnixMilli(), created.UnixMilli()
		if act.Valid {
			ms := act.Time.UnixMilli()
			c.ActivatedAt = &ms
			hasActive = true
		}
		items = append(items, c)
	}
	if hasActive { // enrich with the live bonus balance from futures
		var fb struct {
			Items []struct {
				Ref       string `json:"ref"`
				Remaining string `json:"remaining"`
				Status    string `json:"status"`
				ExpiresAt int64  `json:"expiresAt"`
			} `json:"items"`
		}
		if err := s.call(ctx, "GET", s.cfg.FuturesURL+"/internal/bonus?userId="+uid, nil, &fb); err == nil {
			byRef := map[string]*bonusView{}
			for _, b := range fb.Items {
				byRef[b.Ref] = &bonusView{Remaining: b.Remaining, Status: b.Status, ExpiresAt: b.ExpiresAt}
			}
			for _, c := range items {
				c.Bonus = byRef[couponRef(c.ID)]
			}
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type rowQueryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func couponRef(id int64) string { return fmt.Sprintf("coupon:%d", id) }

func (s *Server) handleCouponSummary(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	s.expireCoupons(ctx)
	var issued, active int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status='issued'), count(*) FILTER (WHERE status='active') FROM g_coupons WHERE uid=$1`, uid).Scan(&issued, &active)
	common.WriteJSON(w, http.StatusOK, map[string]any{"available": issued, "active": active})
}

func (s *Server) handleCouponActivate(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, notFound("COUPON_NOT_FOUND", "卡券不存在"))
		return
	}
	var kind, title, status string
	var amt decimal.Decimal
	var by time.Time
	var days int
	err = s.db.QueryRowContext(ctx, `SELECT kind, amount, title, status, activate_by, valid_days FROM g_coupons WHERE id=$1 AND uid=$2`, id, uid).
		Scan(&kind, &amt, &title, &status, &by, &days)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, notFound("COUPON_NOT_FOUND", "卡券不存在"))
		return
	} else if err != nil {
		writeErr(w, err)
		return
	}
	if status == "issued" && by.Before(s.now()) {
		s.expireCoupons(ctx)
		status = "expired"
	}
	if status != "issued" {
		writeErr(w, conflict("COUPON_NOT_ACTIVATABLE", "卡券已激活或已过期"))
		return
	}
	// Futures grant is idempotent on ref, so a retry after a crash is safe.
	if err := s.call(ctx, "POST", s.cfg.FuturesURL+"/internal/bonus/grant", map[string]any{
		"userId": uid, "ref": couponRef(id), "kind": kind, "amount": amt.String(), "validDays": days,
		"source": title, "requireOwnFunds": kind == "trial" && s.cfg.CouponOwnFunds,
	}, nil); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE g_coupons SET status='active', activated_at=now() WHERE id=$1 AND status='issued'`, id); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "active"})
}
